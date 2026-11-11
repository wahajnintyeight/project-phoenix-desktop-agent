package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Class says how an approval is remembered.
type Class string

const (
	// Look is seeing an application (a screenshot, its controls, the window list). One
	// approval covers that application for the rest of the turn.
	Look Class = "look"
	// Act changes something or reads private data (click, type, clipboard, ...). It is asked
	// every time, unless the user chose to allow that tool for this turn.
	Act Class = "act"
)

// Request is one thing the agent wants to do.
type Request struct {
	Class   Class  `json:"class"`
	Tool    string `json:"tool"`
	App     string `json:"app,omitempty"`
	Summary string `json:"summary"` // plain words for the user, built by Phoenix, never by the model
}

// Decision is the user's answer.
type Decision string

const (
	Allow    Decision = "allow"
	AllowAll Decision = "all" // allow this tool on this app for the rest of the turn
	Deny     Decision = "deny"
)

// Approver decides whether a request may go ahead.
type Approver interface {
	Approve(ctx context.Context, r Request) (bool, error)
}

// DenyAll refuses everything; it is what a server with no way to ask the user uses.
type DenyAll struct{}

func (DenyAll) Approve(context.Context, Request) (bool, error) { return false, nil }

// ── The server side: runs inside Phoenix and asks the user ────────────────────────────

// Pending is a request waiting for the user, handed to the Broker's notify function.
type Pending struct {
	ID      string
	RunID   string
	Request Request
}

// Broker receives approval requests from desktop MCP servers over loopback HTTP and turns
// them into Pending items for the UI. Each chat turn gets its own Run with its own secret
// token, so a request from another process or an old turn is refused.
type Broker struct {
	notify func(Pending)

	mu      sync.Mutex
	ln      net.Listener
	runs    map[string]*Run // by token
	pending map[string]*waiter
}

type waiter struct {
	run *Run
	req Request
	ch  chan Decision
}

// Run is the approval state of one chat turn.
type Run struct {
	ID    string
	URL   string // where the MCP server posts requests
	Token string

	mu        sync.Mutex
	allowed   map[string]bool
	automatic bool
	looked    map[string]bool
	done      chan struct{}
}

// NewBroker creates a broker. notify is called from a server goroutine for every request
// that needs the user; it must be quick and hand off to the UI thread itself.
func NewBroker(notify func(Pending)) *Broker {
	return &Broker{notify: notify, runs: map[string]*Run{}, pending: map[string]*waiter{}}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewRun starts the approval state for one turn. The run ends when ctx does: its token stops
// working and anything still waiting is denied.
func (b *Broker) NewRun(ctx context.Context) (*Run, error) {
	b.mu.Lock()
	if b.ln == nil {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			b.mu.Unlock()
			return nil, err
		}
		b.ln = ln
		srv := &http.Server{Handler: http.HandlerFunc(b.serve), ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
	}
	r := &Run{
		ID: randomHex(6), Token: randomHex(32), looked: map[string]bool{}, allowed: map[string]bool{}, done: make(chan struct{}),
		URL: "http://" + b.ln.Addr().String() + "/approve",
	}
	b.runs[r.Token] = r
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.runs, r.Token)
		var stale []*waiter
		for id, w := range b.pending {
			if w.run == r {
				stale = append(stale, w)
				delete(b.pending, id)
			}
		}
		b.mu.Unlock()
		close(r.done)
		for _, w := range stale {
			w.ch <- Deny // buffered: never blocks
		}
	}()
	return r, nil
}

// NewAutomaticRun keeps per-run authentication but permits tools without interrupting the user.
func (b *Broker) NewAutomaticRun(ctx context.Context) (*Run, error) {
	r, err := b.NewRun(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.automatic = true
	r.mu.Unlock()
	return r, nil
}

// Respond delivers the user's answer for a pending request. It reports whether the request
// was still waiting.
func (b *Broker) Respond(id string, d Decision) bool {
	b.mu.Lock()
	w, ok := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if !ok {
		return false
	}
	if d != Allow && d != AllowAll {
		d = Deny
	}
	w.ch <- d
	return true
}

func (b *Broker) serve(rw http.ResponseWriter, r *http.Request) {
	// Only a process on this machine that was given the token may ask. Checking the Host
	// header as well stops a web page from reaching the port through DNS rebinding.
	host, _, _ := net.SplitHostPort(r.Host)
	if r.Method != http.MethodPost || r.URL.Path != "/approve" || host != "127.0.0.1" {
		http.NotFound(rw, r)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	b.mu.Lock()
	var run *Run
	for t, candidate := range b.runs {
		if subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1 {
			run = candidate
		}
	}
	b.mu.Unlock()
	if run == nil {
		http.Error(rw, "unknown run", http.StatusUnauthorized)
		return
	}
	var req Request
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil || (req.Class != Look && req.Class != Act) {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}
	req.Summary = clipText(req.Summary, 300)
	req.App, req.Tool = clipText(req.App, 100), clipText(req.Tool, 60)

	d := b.decide(r.Context(), run, req)
	_ = json.NewEncoder(rw).Encode(map[string]string{"decision": string(d)})
}

func (b *Broker) decide(ctx context.Context, run *Run, req Request) Decision {
	select {
	case <-ctx.Done():
		return Deny
	case <-run.done:
		return Deny
	default:
	}
	run.mu.Lock()
	switch {
	case run.automatic:
		run.mu.Unlock()
		return Allow
	case req.Class == Look && run.looked[req.App]:
		run.mu.Unlock()
		return Allow
	case req.Class == Act && run.allowed[req.App+"\x00"+req.Tool]:
		run.mu.Unlock()
		return Allow
	}
	run.mu.Unlock()

	w := &waiter{run: run, req: req, ch: make(chan Decision, 1)}
	id := randomHex(6)
	b.mu.Lock()
	b.pending[id] = w
	b.mu.Unlock()
	b.notify(Pending{ID: id, RunID: run.ID, Request: req})

	var d Decision
	select {
	case d = <-w.ch:
	case <-ctx.Done():
		d = Deny
	case <-time.After(5 * time.Minute):
		d = Deny
	}
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()

	run.mu.Lock()
	defer run.mu.Unlock()
	if d == Allow || d == AllowAll {
		if req.Class == Look {
			run.looked[req.App] = true
		}
		if d == AllowAll {
			run.allowed[req.App+"\x00"+req.Tool] = true
			run.looked[req.App] = true
		}
		return Allow
	}
	return Deny
}

func clipText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

// ── The client side: runs inside the MCP server process ───────────────────────────────

// HTTPApprover asks a Broker. Any failure to reach it counts as a refusal.
type HTTPApprover struct {
	URL, Token string
	Client     *http.Client
}

func (a HTTPApprover) Approve(ctx context.Context, r Request) (bool, error) {
	body, _ := json.Marshal(r)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	c := a.Client
	if c == nil {
		c = &http.Client{Timeout: 6 * time.Minute}
	}
	resp, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, errors.New("Phoenix refused the request")
	}
	var out struct {
		Decision Decision `json:"decision"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return false, err
	}
	return out.Decision == Allow, nil
}
