package providerhub

import (
	"context"
	"os"
	"strings"
	"testing"

	"phoenix/internal/mcp"
	"phoenix/internal/provider"
)

type captureRun struct {
	fake
	req provider.Request
	cfg string
}

func (c *captureRun) Run(_ context.Context, _ provider.Settings, req provider.Request, _ func(provider.Event)) (provider.Result, error) {
	c.req = req
	if req.MCPConfig != "" {
		b, _ := os.ReadFile(req.MCPConfig)
		c.cfg = string(b)
	}
	return provider.Result{Text: "ok"}, nil
}

func TestMCPLifecycle(t *testing.T) {
	h, _, _, path := newHub(t)
	err := h.MCPAdd(mcp.Server{Name: "files", Transport: mcp.Stdio, Command: "npx", Args: []string{"-y", "srv"}},
		map[string]string{"TOKEN": "s3cret-value"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.MCPAdd(mcp.Server{Name: "files", Transport: mcp.Stdio, Command: "other"}, nil, nil); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := h.MCPAdd(mcp.Server{Name: "bad", Transport: mcp.HTTP, URL: "http://evil.example/x"}, nil, nil); err == nil {
		t.Fatal("insecure URL accepted")
	}

	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "s3cret-value") || !strings.Contains(string(raw), "vault:mcp-files-e0") {
		t.Fatalf("config must hold a reference only:\n%s", raw)
	}
	if got := h.MCPServers(); len(got) != 1 || got[0].Enabled {
		t.Fatalf("a new server must start disabled: %+v", got)
	}
	if !h.vault.Has("mcp-files-e0") {
		t.Fatal("secret not in vault")
	}

	if err := h.MCPSetEnabled("files", true); err != nil {
		t.Fatal(err)
	}
	if err := h.MCPSetEnabled("nope", true); err == nil {
		t.Fatal("enabling a missing server should fail")
	}
	if len(h.MCPEnabled()) != 1 {
		t.Fatal("not enabled")
	}

	if err := h.MCPRemove("files"); err != nil {
		t.Fatal(err)
	}
	if len(h.MCPServers()) != 0 || h.vault.Has("mcp-files-e0") {
		t.Fatal("remove should drop the entry and its secrets")
	}
}

func TestRunAttachesEnabledServersForClaudeOnly(t *testing.T) {
	h, _, _, _ := newHub(t)
	cl := &captureRun{fake: fake{id: provider.Claude}}
	cx := &captureRun{fake: fake{id: provider.Codex}}
	h.providers[provider.Claude], h.providers[provider.Codex] = cl, cx

	_ = h.MCPAdd(mcp.Server{Name: "files", Transport: mcp.Stdio, Command: "npx"}, map[string]string{"TOKEN": "s3cret-value"}, nil)
	_ = h.MCPAdd(mcp.Server{Name: "off", Transport: mcp.Stdio, Command: "x"}, nil, nil)
	_ = h.MCPSetEnabled("files", true)

	// Without UseMCP nothing is attached (the Test button, quick checks).
	if _, err := h.Run(context.Background(), provider.Claude, provider.Request{Prompt: "hi"}, func(provider.Event) {}); err != nil || cl.req.MCPConfig != "" {
		t.Fatalf("attached without UseMCP: %v %+v", err, cl.req)
	}
	if _, err := h.Run(context.Background(), provider.Claude, provider.Request{Prompt: "hi", UseMCP: true}, func(provider.Event) {}); err != nil {
		t.Fatal(err)
	}
	if cl.req.MCPConfig == "" || len(cl.req.AllowedTools) != 1 || cl.req.AllowedTools[0] != "mcp__files" {
		t.Fatalf("%+v", cl.req)
	}
	if !strings.Contains(cl.cfg, "s3cret-value") || strings.Contains(cl.cfg, `"off"`) {
		t.Fatalf("config handed to the CLI: %s", cl.cfg)
	}
	if _, err := os.Stat(cl.req.MCPConfig); !os.IsNotExist(err) {
		t.Fatal("the private config file must be removed after the run")
	}
	if _, err := h.Run(context.Background(), provider.Codex, provider.Request{Prompt: "hi", UseMCP: true}, func(provider.Event) {}); err != nil || cx.req.MCPConfig != "" {
		t.Fatalf("codex must not get a Claude config: %v %+v", err, cx.req)
	}
}
