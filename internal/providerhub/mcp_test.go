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

func TestClaudeProxyConfigIsPrivateAndRemoved(t *testing.T) {
	proxy := &codexToolProxy{url: "http://127.0.0.1:1234/mcp", token: "test-token"}
	req := provider.Request{}
	cleanup, err := attachClaudeProxy(&req, proxy)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(req.MCPConfig)
	if err != nil || !strings.Contains(string(raw), "test-token") || !strings.Contains(string(raw), "phoenix-tools") {
		t.Fatalf("invalid config: %s (%v)", raw, err)
	}
	if len(req.AllowedTools) != 1 || req.AllowedTools[0] != "mcp__phoenix-tools" {
		t.Fatalf("allowed tools: %v", req.AllowedTools)
	}
	cleanup()
	if _, err := os.Stat(req.MCPConfig); !os.IsNotExist(err) {
		t.Fatal("temporary config was not removed")
	}
}
