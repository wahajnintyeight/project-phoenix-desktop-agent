package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"phoenix/internal/browser"
	"phoenix/internal/desktop"
	"phoenix/internal/mcp"
)

type browserRun struct {
	token    string
	ctx      context.Context
	approval *desktop.Run
}

type browserBridge struct {
	mu   sync.Mutex
	ln   net.Listener
	runs map[string]*browserRun
}

var browserBridgeOnce sync.Once
var browserBridgeInstance *browserBridge

func browserBroker() *browserBridge {
	browserBridgeOnce.Do(func() { browserBridgeInstance = &browserBridge{runs: make(map[string]*browserRun)} })
	return browserBridgeInstance
}

func (b *browserBridge) NewRun(ctx context.Context, approval *desktop.Run) (mcp.Resolved, error) {
	b.mu.Lock()
	if b.ln == nil {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			b.mu.Unlock()
			return mcp.Resolved{}, err
		}
		b.ln = ln
		srv := &http.Server{Handler: http.HandlerFunc(b.serve), ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
	}
	exe, err := osExecutable()
	if err != nil {
		b.mu.Unlock()
		return mcp.Resolved{}, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		b.mu.Unlock()
		return mcp.Resolved{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	run := &browserRun{token: token, ctx: ctx, approval: approval}
	b.runs[token] = run
	url := "http://" + b.ln.Addr().String() + "/browser/tool"
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.runs, token)
		b.mu.Unlock()
	}()
	return mcp.Resolved{
		Server:    mcp.Server{Name: browser.ServerName, Transport: mcp.Stdio, Command: exe, Args: []string{"mcp-browser"}, Enabled: true},
		EnvValues: map[string]string{"PHOENIX_BROWSER_URL": url, "PHOENIX_BROWSER_TOKEN": token},
	}, nil
}

func (b *browserBridge) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/browser/tool" || r.URL.RawQuery != "" || !browser.RemoteHostIsLoopback(r) {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Origin") != "" {
		http.Error(w, "browser bridge is local-process only", http.StatusForbidden)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	b.mu.Lock()
	run := b.runs[token]
	b.mu.Unlock()
	if run == nil || len(run.token) != len(token) || subtle.ConstantTimeCompare([]byte(run.token), []byte(token)) != 1 {
		http.Error(w, "unknown browser run", http.StatusUnauthorized)
		return
	}
	var call browser.Call
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&call); err != nil || !knownBrowserTool(call.Tool) {
		http.Error(w, "invalid browser operation", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Minute)
	defer cancel()
	stopCancel := context.AfterFunc(run.ctx, cancel)
	defer stopCancel()
	meta, err := browserCallMeta(ctx, call)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if run.approval == nil {
		http.Error(w, "Phoenix could not start its approval service", http.StatusServiceUnavailable)
		return
	}
	approver := desktop.HTTPApprover{URL: run.approval.URL, Token: run.approval.Token}
	allowed, err := approver.Approve(ctx, desktop.Request{
		Class: meta.Class, Tool: call.Tool, App: meta.App, Summary: meta.Summary,
	})
	if err != nil || !allowed {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(browser.Output{Text: "The user did not allow this. Do not try it again; tell them what you wanted to do.", IsError: true})
		return
	}
	if ctx.Err() != nil {
		http.Error(w, "browser operation was canceled", http.StatusRequestTimeout)
		return
	}
	operationCtx, operationCancel := context.WithTimeout(ctx, 45*time.Second)
	defer operationCancel()
	result, err := performBrowserTool(operationCtx, call)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(browser.Output{Text: "Browser operation failed: " + err.Error(), IsError: true})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func knownBrowserTool(name string) bool {
	switch name {
	case "browser_tabs", "browser_snapshot", "browser_screenshot", "browser_navigate", "browser_click",
		"browser_type", "browser_fill", "browser_press", "browser_scroll", "browser_wait", "browser_select",
		"browser_back", "browser_forward", "browser_reload", "browser_open_tab", "browser_close_tab", "browser_switch_tab":
		return true
	}
	return false
}

func browserServer(ctx context.Context, approval *desktop.Run) (mcp.Resolved, error) {
	return browserBroker().NewRun(ctx, approval)
}

func osExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", errors.New("could not locate Phoenix for its browser tool server")
	}
	return path, nil
}
