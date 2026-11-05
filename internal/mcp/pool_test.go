package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoArgs struct {
	Value string `json:"value"`
}

// fake is an in-process server that counts how many times it was started.
func fake(name string, starts *atomic.Int32, tools ...string) Resolved {
	return Resolved{
		Server: Server{Name: name, Transport: Stdio, Enabled: true},
		InProcess: func() *sdk.Server {
			starts.Add(1)
			s := sdk.NewServer(&sdk.Implementation{Name: name, Version: "1"}, nil)
			for _, t := range tools {
				sdk.AddTool(s, &sdk.Tool{Name: t, Description: "echo"}, func(_ context.Context, _ *sdk.CallToolRequest, a echoArgs) (*sdk.CallToolResult, any, error) {
					return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "echo " + a.Value}}}, nil, nil
				})
			}
			return s
		},
	}
}

func ctxT(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPoolReusesConnectionAcrossRuns(t *testing.T) {
	var starts atomic.Int32
	p := NewPool()
	defer p.Close()
	srv := fake("notes", &starts, "search")
	for i := 0; i < 3; i++ {
		rt, missing, err := p.OpenAvailable(ctxT(t), []Resolved{srv}, nil)
		if err != nil || len(missing) != 0 {
			t.Fatalf("run %d: %v %v", i, err, missing)
		}
		fns := rt.Functions()
		if len(fns) != 1 || fns[0].Name != "notes_search" || fns[0].Server != "notes" || fns[0].MCPName != "search" {
			t.Fatalf("functions = %+v", fns)
		}
		res, err := rt.Call(ctxT(t), fns[0].Name, []byte(`{"value":"x"}`))
		if err != nil || res.Text != "echo x" {
			t.Fatalf("call: %v %+v", err, res)
		}
		rt.Close() // must not close the pooled session
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("server started %d times, want 1", got)
	}
}

func TestPoolWarmConnectsBeforeFirstRun(t *testing.T) {
	var starts atomic.Int32
	p := NewPool()
	defer p.Close()
	p.Warm([]Resolved{fake("a", &starts, "t")})
	deadline := time.Now().Add(5 * time.Second)
	for starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if starts.Load() != 1 {
		t.Fatal("Warm did not start the server")
	}
}

func TestPoolReconnectsAfterChangeAndDrop(t *testing.T) {
	var starts atomic.Int32
	p := NewPool()
	defer p.Close()
	a := fake("a", &starts, "t")
	if _, _, err := p.OpenAvailable(ctxT(t), []Resolved{a}, nil); err != nil {
		t.Fatal(err)
	}
	// A different secret is a different connection.
	b := a
	b.EnvValues = map[string]string{"KEY": "v2"}
	if _, _, err := p.OpenAvailable(ctxT(t), []Resolved{b}, nil); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 2 {
		t.Fatalf("starts = %d after a config change, want 2", starts.Load())
	}
	// The server going away is noticed, and the next run reconnects.
	p.mu.Lock()
	e := p.entries["a"]
	p.mu.Unlock()
	_ = e.session.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		_, still := p.entries["a"]
		p.mu.Unlock()
		if !still {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	rt, missing, err := p.OpenAvailable(ctxT(t), []Resolved{b}, nil)
	if err != nil || len(missing) != 0 || len(rt.Functions()) != 1 || starts.Load() != 3 {
		t.Fatalf("after drop: %v %v starts=%d", err, missing, starts.Load())
	}
}

func TestPoolSyncDropsRemovedServers(t *testing.T) {
	var starts atomic.Int32
	p := NewPool()
	defer p.Close()
	a := fake("a", &starts, "t")
	if _, _, err := p.OpenAvailable(ctxT(t), []Resolved{a}, nil); err != nil {
		t.Fatal(err)
	}
	p.Sync(nil)
	p.mu.Lock()
	n := len(p.entries)
	p.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d entries left after Sync(nil)", n)
	}
}

func TestPoolBrokenServerIsSkippedAndNotRetriedImmediately(t *testing.T) {
	p := NewPool()
	defer p.Close()
	var starts atomic.Int32
	good := fake("good", &starts, "t")
	bad := Resolved{Server: Server{Name: "bad", Transport: Stdio, Command: "phoenix-no-such-program-xyz", Enabled: true}}
	rt, missing, err := p.OpenAvailable(ctxT(t), []Resolved{bad, good}, nil)
	if err != nil || len(missing) != 1 || missing[0] != "bad" || len(rt.Functions()) != 1 {
		t.Fatalf("got %v %v %d tools", err, missing, len(rt.Functions()))
	}
	started := time.Now()
	if _, missing, _ = p.OpenAvailable(ctxT(t), []Resolved{bad, good}, nil); len(missing) != 1 {
		t.Fatalf("missing = %v", missing)
	}
	if time.Since(started) > time.Second {
		t.Fatal("a recently failed server was retried instead of skipped")
	}
}

func TestEphemeralServersAreClosedWithTheRunAndKeepPlainNames(t *testing.T) {
	var starts atomic.Int32
	p := NewPool()
	defer p.Close()
	pooled := fake("browser", &starts, "browser_click") // a user server may reuse a name
	own := fake("phoenix-browser", &starts, "browser_click")
	rt, missing, err := p.OpenAvailable(ctxT(t), []Resolved{pooled}, []Resolved{own})
	if err != nil || len(missing) != 0 {
		t.Fatal(err, missing)
	}
	names := map[string]string{}
	for _, f := range rt.Functions() {
		names[f.Name] = f.Server
	}
	if names["browser_click"] != "phoenix-browser" || names["browser_browser_click"] != "browser" {
		t.Fatalf("names = %v", names)
	}
	rt.Close()
	if _, err := rt.Call(ctxT(t), "browser_click", []byte(`{}`)); err == nil {
		t.Fatal("call succeeded on a closed per-run session")
	}
}

func TestAliasNamesAreSafeAndUnique(t *testing.T) {
	used := map[string]bool{}
	a := aliasName("my.server", "create issue!", false, used)
	b := aliasName("my.server", "create issue!", false, used)
	if a != "my_server_create_issue_" || b == a {
		t.Fatalf("%q %q", a, b)
	}
	long := aliasName("s", string(make([]byte, 200)), false, map[string]bool{})
	if len(long) > 64 {
		t.Fatalf("len = %d", len(long))
	}
}

func TestRunDoesNotWaitForASlowServer(t *testing.T) {
	var starts atomic.Int32
	p := NewPool()
	defer p.Close()
	fast := fake("fast", &starts, "t")
	slowBase := fake("slow", &starts, "t")
	slow := slowBase
	slow.InProcess = func() *sdk.Server {
		time.Sleep(1500 * time.Millisecond) // an npx download, say
		return slowBase.InProcess()
	}
	began := time.Now()
	rt, missing, starting, err := p.OpenRun(ctxT(t), []Resolved{fast, slow}, nil, 150*time.Millisecond)
	if err != nil || len(missing) != 0 {
		t.Fatal(err, missing)
	}
	if time.Since(began) > time.Second {
		t.Fatalf("the run waited %v for a slow server", time.Since(began))
	}
	if len(starting) != 1 || starting[0] != "slow" || len(rt.Functions()) != 1 {
		t.Fatalf("starting=%v tools=%d", starting, len(rt.Functions()))
	}
	// Its connection carried on in the background: the next run has it, with no new start.
	rt2, _, starting2, err := p.OpenRun(ctxT(t), []Resolved{fast, slow}, nil, 5*time.Second)
	if err != nil || len(starting2) != 0 || len(rt2.Functions()) != 2 {
		t.Fatalf("second run: %v starting=%v tools=%d", err, starting2, len(rt2.Functions()))
	}
	if starts.Load() != 2 {
		t.Fatalf("starts = %d, want 2 (each server once)", starts.Load())
	}
}
