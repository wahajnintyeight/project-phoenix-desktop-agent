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

// A server can be renamed and can change from a program to a web address (and back); its
// credentials follow the change and nothing is left behind in the vault.
func TestMCPUpdateRenamesAndSwitchesType(t *testing.T) {
	h, _, _, path := newHub(t)
	if err := h.MCPAdd(mcp.Server{Name: "files", Transport: mcp.Stdio, Command: "npx", Args: []string{"srv"}},
		map[string]string{"TOKEN": "env-secret"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.MCPSetEnabled("files", true); err != nil {
		t.Fatal(err)
	}
	if err := h.MCPAdd(mcp.Server{Name: "other", Transport: mcp.Stdio, Command: "x"}, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Rename only: the credential is kept (blank value), the enabled state is kept.
	err := h.MCPUpdate("files", mcp.Server{Name: "docs", Transport: mcp.Stdio, Command: "npx", Args: []string{"srv"}},
		map[string]string{"TOKEN": ""}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := h.MCPServers()
	names := map[string]mcp.Server{}
	for _, s := range got {
		names[s.Name] = s
	}
	if _, old := names["files"]; old || !names["docs"].Enabled || names["docs"].Env["TOKEN"] == "" {
		t.Fatalf("rename should move the server, keep it on and keep its credential: %+v", got)
	}
	if err := h.MCPUpdate("docs", mcp.Server{Name: "other", Transport: mcp.Stdio, Command: "npx"}, nil, nil); err == nil {
		t.Fatal("a rename must not take another server's name")
	}

	// Program -> web address: the environment credential is dropped, headers are stored.
	err = h.MCPUpdate("docs", mcp.Server{Name: "docs", Transport: mcp.HTTP, URL: "https://example.com/mcp"},
		nil, map[string]string{"Authorization": "Bearer header-secret"})
	if err != nil {
		t.Fatal(err)
	}
	var docs mcp.Server
	for _, s := range h.MCPServers() {
		if s.Name == "docs" {
			docs = s
		}
	}
	if docs.Transport != mcp.HTTP || docs.Command != "" || len(docs.Env) != 0 || docs.Headers["Authorization"] == "" || !docs.Enabled {
		t.Fatalf("type switch left the wrong fields: %+v", docs)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "env-secret") || strings.Contains(string(raw), "header-secret") {
		t.Fatalf("config must hold references only:\n%s", raw)
	}
	if h.vault.Has("mcp-files-e0") {
		t.Fatal("the old environment credential should have been deleted")
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
