package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phoenix/internal/provider"
)

func TestMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Providers.Default != "claude" || c.Provider(provider.Claude).Auth != provider.AuthCLI || c.Provider(provider.Codex).Auth != provider.AuthCLI {
		t.Fatalf("%+v", c.Providers)
	}
}

func TestRoundTripKeepsUnknownSections(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	src := `storage:
  data_dir: D:\Phoenix
future_section:
  - name: resume-matcher
providers:
  default: codex
  claude:
    auth: api_key
    api_key: vault:provider-claude
    model: sonnet
  codex:
    auth: cli
`
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Providers.Default != "codex" || c.Provider(provider.Claude).Auth != provider.AuthAPIKey || c.Provider(provider.Claude).Model != "sonnet" {
		t.Fatalf("%+v", c.Providers)
	}
	cl := c.Provider(provider.Claude)
	cl.Auth = provider.AuthCLI
	c.SetProvider(provider.Claude, cl)
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	for _, want := range []string{"data_dir:", "resume-matcher", "default: codex", "vault:provider-claude"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rewritten file lost %q:\n%s", want, out)
		}
	}
	again, err := Load(p)
	if err != nil || again.Provider(provider.Claude).Auth != provider.AuthCLI {
		t.Fatalf("reload: %v %+v", err, again.Providers)
	}
}

func TestInvalidValuesAreNormalized(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("providers:\n  default: nonsense\n  claude:\n    auth: root\n"), 0o600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Providers.Default != "claude" || c.Provider(provider.Claude).Auth != provider.AuthCLI {
		t.Fatalf("%+v", c.Providers)
	}
}

func TestMalformedYAMLIsAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("providers: [unclosed"), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("expected an error")
	}
}

func TestIsLiteralKey(t *testing.T) {
	for ref, want := range map[string]bool{"": false, "vault:x": false, "env:X": false, "sk-abc": true} {
		if IsLiteralKey(ref) != want {
			t.Errorf("%q", ref)
		}
	}
}

func TestSearchURLValidation(t *testing.T) {
	for _, ok := range []string{"https://duckduckgo.com/?q={query}", "https://www.google.com/search?q={query}", "https://a.b/{query}"} {
		if !ValidSearchURL(ok) {
			t.Errorf("rejected %q", ok)
		}
	}
	for _, bad := range []string{"", "http://x.com/?q={query}", "javascript:alert({query})", "file:///c:/{query}",
		"https://x.com/", "https://x.com/{query}{query}", "https://u:p@x.com/{query}", "https:///{query}", "//x.com/{query}"} {
		if ValidSearchURL(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestStoreUpdateWritesAndNormalizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Config) {
		c.Browser.SearchURL = "http://insecure/{query}" // invalid: must be dropped
		pc := c.Provider("claude")
		pc.Model, pc.Effort = "--bad", "High"
		c.SetProvider("claude", pc)
	}); err != nil {
		t.Fatal(err)
	}
	s2, _ := OpenStore(path)
	s2.View(func(c *Config) {
		if c.SearchURL() != DefaultSearchURL || c.Providers.Claude.Model != "" || c.Providers.Claude.Effort != "" {
			t.Errorf("not normalized: %+v", c)
		}
	})
}

func TestHTTPProvidersRoundTripAndAreSanitized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	raw := "providers:\n  default: grok\n  gemini:\n    auth: cli\n    model: gemini-x\n    base_url: https://proxy.example/v1\n" +
		"  mistral:\n    base_url: http://evil.example/v1\n    model: --bad\n  nonsense:\n    model: x\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	g := c.Provider("gemini")
	if g.Auth != provider.AuthAPIKey || g.Model != "gemini-x" || g.BaseURL != "https://proxy.example/v1" {
		t.Errorf("gemini: %+v", g)
	}
	if m := c.Provider("mistral"); m.BaseURL != "" || m.Model != "" {
		t.Errorf("an insecure base URL or a flag-like model survived: %+v", m)
	}
	if _, ok := c.Providers.Others["nonsense"]; ok {
		t.Error("unknown provider key kept")
	}
	if c.Providers.Default != "grok" {
		t.Errorf("default %q", c.Providers.Default)
	}
	if c.Provider("zai").Auth != provider.AuthAPIKey {
		t.Error("an unconfigured HTTP provider must default to API-key mode")
	}

	c.SetProvider("zai", ProviderConfig{Model: "glm-x", BaseURL: "https://open.bigmodel.cn/api/paas/v4"})
	c.SetProvider("evil", ProviderConfig{Model: "x"})
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	c2, _ := Load(path)
	if z := c2.Provider("zai"); z.Model != "glm-x" || z.BaseURL == "" || z.Auth != provider.AuthAPIKey {
		t.Errorf("zai after reload: %+v", z)
	}
	if _, ok := c2.Providers.Others["evil"]; ok {
		t.Error("unknown provider stored")
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "nonsense") {
		t.Error("stray key written back")
	}
}

func TestMCPServersRoundTripAndValidate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	src := `mcp_servers:
  files:
    transport: stdio
    command: npx
    args: [-y, some-server]
    env:
      TOKEN: vault:mcp-files-e0
    enabled: true
  remote:
    transport: http
    url: https://example.com/mcp
  evil:
    transport: http
    url: http://attacker.example/mcp
  Bad_Name:
    transport: stdio
    command: x
`
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.MCPServers) != 2 || c.MCPServers["files"].Name != "files" || !c.MCPServers["files"].Enabled || c.MCPServers["remote"].Enabled {
		t.Fatalf("%+v", c.MCPServers)
	}
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if strings.Contains(string(out), "attacker") || strings.Contains(string(out), "name:") || !strings.Contains(string(out), "vault:mcp-files-e0") {
		t.Fatalf("saved file:\n%s", out)
	}
}
