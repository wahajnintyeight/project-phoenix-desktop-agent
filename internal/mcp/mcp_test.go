package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain doubles as a tiny MCP server: started with PHOENIX_TEST_MCP=1 the test binary
// serves two tools over stdio, so Test is exercised against a real child process.
func TestMain(m *testing.M) {
	if os.Getenv("PHOENIX_TEST_MCP") == "1" {
		srv := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1.2"}, nil)
		type in struct{}
		sdk.AddTool(srv, &sdk.Tool{Name: "zeta", Description: "last"}, func(_ context.Context, _ *sdk.CallToolRequest, _ in) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: os.Getenv("FIXTURE_SECRET")}}}, nil, nil
		})
		sdk.AddTool(srv, &sdk.Tool{Name: "alpha", Description: "first"}, func(_ context.Context, _ *sdk.CallToolRequest, _ in) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{}, nil, nil
		})
		_ = srv.Run(context.Background(), &sdk.StdioTransport{})
		return
	}
	os.Exit(m.Run())
}

func TestValidate(t *testing.T) {
	good := []Server{
		{Name: "files", Transport: Stdio, Command: "npx", Args: []string{"-y", "pkg"}},
		{Name: "a", Transport: HTTP, URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "vault:x"}},
		{Name: "local", Transport: HTTP, URL: "http://127.0.0.1:8080/mcp"},
	}
	for _, s := range good {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []Server{
		{Name: "Bad_Name", Transport: Stdio, Command: "x"},
		{Name: "a__b", Transport: Stdio, Command: "x"},
		{Name: "-a", Transport: Stdio, Command: "x"},
		{Name: "a", Transport: Stdio},
		{Name: "a", Transport: Stdio, Command: "x\ny"},
		{Name: "a", Transport: Stdio, Command: "x", Env: map[string]string{"1BAD": "v"}},
		{Name: "a", Transport: Stdio, Command: "x", URL: "https://e.com"},
		{Name: "a", Transport: HTTP, URL: "http://evil.com/mcp"},
		{Name: "a", Transport: HTTP, URL: "https://user:pw@e.com/"},
		{Name: "a", Transport: HTTP, URL: "ftp://e.com"},
		{Name: "a", Transport: HTTP, URL: "https://e.com", Command: "x"},
		{Name: "a", Transport: "pipe", Command: "x"},
		{Name: "a", Transport: HTTP, URL: "https://e.com", Headers: map[string]string{"bad header": "v"}},
	}
	for _, s := range bad {
		if s.Validate() == nil {
			t.Errorf("accepted %+v", s)
		}
	}
}

func TestCleanMap(t *testing.T) {
	in := map[string]Server{
		"a":   {Transport: Stdio, Command: "x"},
		"Bad": {Transport: Stdio, Command: "x"},
		"b":   {Name: "ignored", Transport: Stdio, Command: "x"},
		"c":   {Transport: HTTP, URL: "http://evil.example/"},
	}
	out := CleanMap(in)
	if len(out) != 2 || out["a"].Name != "a" || out["b"].Name != "b" {
		t.Fatalf("%+v", out)
	}
	if l := List(out); l[0].Name != "a" || l[1].Name != "b" {
		t.Fatalf("%+v", l)
	}
	if CleanMap(nil) != nil {
		t.Fatal("empty should be nil")
	}
}

func TestTestOverStdio(t *testing.T) {
	exe, _ := os.Executable()
	s := Resolved{
		Server:    Server{Name: "fixture", Transport: Stdio, Command: exe, Env: map[string]string{"PHOENIX_TEST_MCP": "vault:x"}},
		EnvValues: map[string]string{"PHOENIX_TEST_MCP": "1"},
	}
	info, err := Test(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "fixture" || info.Version != "1.2" || len(info.Tools) != 2 || info.Tools[0].Name != "alpha" || info.Tools[1].Name != "zeta" {
		t.Fatalf("%+v", info)
	}
}

func TestTestFailureDoesNotLeakSecrets(t *testing.T) {
	s := Resolved{
		Server:    Server{Name: "x", Transport: Stdio, Command: "definitely-not-a-program-sk-secret-123"},
		EnvValues: map[string]string{"K": "sk-secret-123"},
	}
	_, err := Test(context.Background(), s)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-secret-123") && strings.Contains(err.Error(), "•••") == false {
		t.Fatalf("secret leaked: %v", err)
	}
}

func TestResolveAndClaudeConfig(t *testing.T) {
	vals := map[string]string{"vault:tok": "T0KEN", "vault:key": "KEY1"}
	r := func(ref string) (string, error) { return vals[ref], nil }
	a, err := Resolve(Server{Name: "a", Transport: Stdio, Command: "npx", Args: []string{"-y", "p"}, Env: map[string]string{"API": "vault:key"}}, r)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Resolve(Server{Name: "b", Transport: HTTP, URL: "https://e.com/mcp", Headers: map[string]string{"Authorization": "vault:tok"}}, r)
	raw, err := ClaudeConfig([]Resolved{a, b})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.MCPServers["a"]["type"] != "stdio" || got.MCPServers["a"]["command"] != "npx" || got.MCPServers["a"]["env"].(map[string]any)["API"] != "KEY1" {
		t.Fatalf("%s", raw)
	}
	if got.MCPServers["b"]["type"] != "http" || got.MCPServers["b"]["headers"].(map[string]any)["Authorization"] != "T0KEN" {
		t.Fatalf("%s", raw)
	}
	if al := ClaudeAllowed([]Resolved{a, b}); len(al) != 2 || al[0] != "mcp__a" {
		t.Fatalf("%v", al)
	}
}

func TestParseImport(t *testing.T) {
	got, err := ParseImport(`{"mcpServers": {
		"My_Files": {"command": "npx", "args": ["-y", "@scope/pkg"], "env": {"TOKEN": "abc12345"}},
		"remote":   {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer xyz"}}
	}}`)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	if got[0].Name != "my-files" || got[0].Transport != Stdio || got[0].EnvValues["TOKEN"] != "abc12345" || got[0].Env != nil {
		t.Fatalf("%+v", got[0])
	}
	if got[1].Name != "remote" || got[1].Transport != HTTP || got[1].HeaderValues["Authorization"] != "Bearer xyz" {
		t.Fatalf("%+v", got[1])
	}
	if bare, err := ParseImport(`{"x": {"command": "node", "args": ["a.js"]}}`); err != nil || len(bare) != 1 {
		t.Fatalf("bare map: %v", err)
	}

	for _, bad := range []string{
		``, `nope`, `{}`, `{"mcpServers": {}}`,
		`{"mcpServers": {"a": {"type": "sse", "url": "https://e.com/sse"}}}`,
		`{"mcpServers": {"a": {}}}`,
		`{"mcpServers": {"a": {"command": "x", "url": "https://e.com"}}}`,
		`{"mcpServers": {"a": {"url": "http://evil.example/mcp"}}}`,
		`{"mcpServers": {"a b": {"command": "x"}, "a-b": {"command": "y"}}}`,
		`{"mcpServers": {"!!!": {"command": "x"}}}`,
		`{"mcpServers": {"a": {"command": "x", "args": [1]}}}`,
	} {
		if _, err := ParseImport(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
