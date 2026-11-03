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
mcp_servers:
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
