package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Pool keeps the user's MCP servers connected for the life of the app. Starting a server (an
// npx download, a Python venv, a TLS handshake) and listing its tools is slow, so it is done
// once at launch, not once per chat message: a run takes the already-open session and its
// cached tool list. A server that dies is reconnected on next use, and one that failed
// recently is skipped for a short while so a broken entry cannot stall every message.
type Pool struct {
	// Trace, when set, receives one line per connect with its duration (never a secret).
	Trace func(format string, args ...any)

	mu      sync.Mutex
	entries map[string]*poolEntry
	closed  bool
}

type poolEntry struct {
	fp      string // fingerprint of the resolved config; a change means reconnect
	ready   chan struct{}
	session *sdk.ClientSession
	cleanup func()
	tools   []poolTool
	err     error
	failed  time.Time
	stale   bool // the server said its tool list changed
}

type poolTool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

const (
	connectTimeout = 90 * time.Second // generous: the first npx run may download a package
	retryAfter     = 20 * time.Second // do not hammer a server that just failed
)

// NewPool returns an empty pool.
func NewPool() *Pool { return &Pool{entries: map[string]*poolEntry{}} }

// fingerprint changes whenever anything that affects the connection changes, including secret
// values (hashed, so the value itself is never kept as a key).
func fingerprint(s Resolved) string {
	h := sha256.New()
	enc := func(parts ...string) {
		for _, p := range parts {
			fmt.Fprintf(h, "%d:%s|", len(p), p)
		}
	}
	enc(string(s.Transport), s.Command, strings.Join(s.Args, "\x00"), s.URL, s.OAuthRef)
	for _, m := range []map[string]string{s.EnvValues, s.HeaderValues} {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			enc(k, m[k])
		}
		enc("--")
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// Warm starts connecting to the servers in the background and returns at once.
func (p *Pool) Warm(servers []Resolved) {
	for _, s := range servers {
		s := s
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
			defer cancel()
			_, _ = p.acquire(ctx, s)
		}()
	}
}

// WarmAndWait connects the servers in parallel and returns how many connected, once each has
// connected or failed (or ctx ends).
func (p *Pool) WarmAndWait(ctx context.Context, servers []Resolved) int {
	var wg sync.WaitGroup
	var ok atomic.Int32
	for _, s := range servers {
		wg.Add(1)
		go func(s Resolved) {
			defer wg.Done()
			if _, err := p.acquire(ctx, s); err == nil {
				ok.Add(1)
			}
		}(s)
	}
	wg.Wait()
	return int(ok.Load())
}

// Sync closes sessions for servers that are no longer wanted (removed, disabled or edited).
func (p *Pool) Sync(servers []Resolved) {
	want := map[string]string{}
	for _, s := range servers {
		want[s.Name] = fingerprint(s)
	}
	p.mu.Lock()
	var drop []*poolEntry
	for name, e := range p.entries {
		if fp, ok := want[name]; !ok || fp != e.fp {
			drop = append(drop, e)
			delete(p.entries, name)
		}
	}
	p.mu.Unlock()
	for _, e := range drop {
		e.close()
	}
}

// Close disconnects everything (app exit). Stdio servers are stopped.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	all := p.entries
	p.entries = map[string]*poolEntry{}
	p.mu.Unlock()
	for _, e := range all {
		e.close()
	}
}

func (e *poolEntry) close() {
	<-e.ready
	if e.session != nil {
		_ = e.session.Close()
	}
	if e.cleanup != nil {
		e.cleanup()
	}
}

// acquire returns a connected entry, connecting if needed. The connection is made with its own
// context so a canceled chat run does not abort a start-up that later runs will want.
func (p *Pool) acquire(ctx context.Context, s Resolved) (*poolEntry, error) {
	fp := fingerprint(s)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("MCP pool is closed")
	}
	e := p.entries[s.Name]
	if e != nil && e.fp != fp {
		delete(p.entries, s.Name)
		old := e
		e = nil
		go old.close()
	}
	if e != nil {
		select {
		case <-e.ready:
			if e.err != nil && time.Since(e.failed) > retryAfter {
				delete(p.entries, s.Name)
				e = nil
			}
		default:
		}
	}
	if e == nil {
		e = &poolEntry{fp: fp, ready: make(chan struct{})}
		p.entries[s.Name] = e
		go p.connect(e, s)
	}
	p.mu.Unlock()

	select {
	case <-e.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if e.err != nil {
		return nil, e.err
	}
	if e.stale {
		if err := p.relist(ctx, e, s); err != nil {
			return nil, err
		}
	}
	return e, nil
}

func (p *Pool) connect(e *poolEntry, s Resolved) {
	started := time.Now()
	defer close(e.ready)
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	fail := func(err error) {
		e.err, e.failed = scrub(err, s), time.Now()
		if p.Trace != nil {
			p.Trace("mcp pool: %s unavailable after %dms", s.Name, time.Since(started).Milliseconds())
		}
	}
	session, cleanup, err := connectSession(ctx, s, func() { e.stale = true })
	if err != nil {
		fail(err)
		return
	}
	tools, err := listTools(ctx, session, s)
	if err != nil {
		_ = session.Close()
		cleanup()
		fail(err)
		return
	}
	e.session, e.cleanup, e.tools = session, cleanup, tools
	if p.Trace != nil {
		p.Trace("mcp pool: %s connected in %dms (%d tools)", s.Name, time.Since(started).Milliseconds(), len(tools))
	}
	// If the server goes away (crash, update, network), forget it so the next use reconnects.
	go func() {
		_ = session.Wait()
		p.mu.Lock()
		if p.entries[s.Name] == e {
			delete(p.entries, s.Name)
		}
		p.mu.Unlock()
		if p.Trace != nil {
			p.Trace("mcp pool: %s disconnected", s.Name)
		}
	}()
}

func (p *Pool) relist(ctx context.Context, e *poolEntry, s Resolved) error {
	tools, err := listTools(ctx, e.session, s)
	if err != nil {
		return err
	}
	p.mu.Lock()
	e.tools, e.stale = tools, false
	p.mu.Unlock()
	return nil
}

func listTools(ctx context.Context, session *sdk.ClientSession, s Resolved) ([]poolTool, error) {
	var out []poolTool
	total := 0
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("could not list tools: %w", err)
		}
		if len(out) >= maxRuntimeTools {
			return nil, fmt.Errorf("the server offers more than %d tools", maxRuntimeTools)
		}
		schema, err := inputSchema(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %s: %w", tool.Name, err)
		}
		total += len(schema)
		if total > maxToolSchemasTotal {
			return nil, errors.New("the tool schemas are too large")
		}
		out = append(out, poolTool{
			Name:        tool.Name,
			Description: clip(redactValues(tool.Description, s), 800),
			Schema:      json.RawMessage(redactValues(string(schema), s)),
		})
	}
	return out, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// aliasName builds the name the model sees: the server and tool, so the name itself says what
// the tool is for (a bare "mcp_tool_017" tells the model nothing). It stays within the 64
// characters providers allow and is unique within the run.
func aliasName(server, tool string, bare bool, used map[string]bool) string {
	base := server + "_" + tool
	if bare { // Phoenix's own tools are already namespaced (browser_click, files_list)
		base = tool
	}
	base = unsafeName.ReplaceAllString(base, "_")
	if len(base) > 56 {
		base = base[:56]
	}
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	used[name] = true
	return name
}

// OpenAvailable assembles one run's tool set, waiting as long as the servers need to connect.
func (p *Pool) OpenAvailable(ctx context.Context, pooled, ephemeral []Resolved) (*Runtime, []string, error) {
	rt, unavailable, starting, err := p.OpenRun(ctx, pooled, ephemeral, 0)
	return rt, append(unavailable, starting...), err
}

// OpenRun assembles one run's tool set. Pooled servers come from their open sessions;
// ephemeral ones (Phoenix's own per-run tools, tied to this run's approvals) are opened now and
// closed with the runtime. Pooled sessions are never closed by Runtime.Close.
//
// wait bounds how long the run waits for pooled servers that are still connecting (0 waits for
// them all). A message should never sit behind a slow npx download: servers that are not ready
// in time are returned as starting, the run goes ahead without them, and their connection
// carries on in the background for the next message. unavailable lists servers that failed.
func (p *Pool) OpenRun(ctx context.Context, pooled, ephemeral []Resolved, wait time.Duration) (rt *Runtime, unavailable, starting []string, err error) {
	r := &Runtime{targets: map[string]target{}}
	used := map[string]bool{}
	total := 0
	add := func(server Resolved, session *sdk.ClientSession, tools []poolTool, bare bool) bool {
		bytes := 0
		for _, t := range tools {
			bytes += len(t.Schema)
		}
		if len(r.functions)+len(tools) > maxRuntimeTools || total+bytes > maxToolSchemasTotal {
			return false
		}
		total += bytes
		for _, t := range tools {
			name := aliasName(server.Name, t.Name, bare, used)
			r.functions = append(r.functions, Function{
				Name: name, Server: server.Name, MCPName: t.Name,
				Description: clip("MCP server "+server.Name+" tool "+t.Name+": "+t.Description, 800),
				Parameters:  t.Schema, RequireApproval: server.RequireApproval,
			})
			r.targets[name] = target{server: server, session: session, tool: t.Name}
		}
		return true
	}

	// Pooled servers connect in parallel; a cold one costs its own start-up, not the sum.
	type result struct {
		i   int
		e   *poolEntry
		err error
	}
	done := make(chan result, len(pooled))
	for i, s := range pooled {
		go func(i int, s Resolved) {
			e, err := p.acquire(ctx, s)
			done <- result{i, e, err}
		}(i, s)
	}
	// Phoenix's own tools open while the pooled servers connect, and are registered first so
	// they keep their plain names.
	for _, s := range ephemeral {
		one, err := Open(ctx, []Resolved{s})
		if err != nil {
			unavailable = append(unavailable, s.Name)
			continue
		}
		var tools []poolTool
		for _, fn := range one.functions {
			tools = append(tools, poolTool{Name: fn.MCPName, Description: strings.TrimPrefix(fn.Description, "MCP server "+s.Name+" tool "+fn.MCPName+": "), Schema: fn.Parameters})
		}
		if !add(s, one.sessions[0], tools, true) {
			one.Close()
			unavailable = append(unavailable, s.Name)
			continue
		}
		r.sessions = append(r.sessions, one.sessions...)
		r.cleanups = append(r.cleanups, one.cleanups...)
	}

	results := make([]*result, len(pooled))
	var timeout <-chan time.Time
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		timeout = t.C
	}
collect:
	for got := 0; got < len(pooled); {
		select {
		case res := <-done:
			results[res.i] = &res
			got++
		case <-timeout:
			break collect
		case <-ctx.Done():
			r.Close()
			return nil, nil, nil, ctx.Err()
		}
	}
	for i, s := range pooled {
		switch res := results[i]; {
		case res == nil:
			starting = append(starting, s.Name)
		case res.err != nil || !add(s, res.e.session, res.e.tools, false):
			unavailable = append(unavailable, s.Name)
		}
	}
	return r, unavailable, starting, nil
}
