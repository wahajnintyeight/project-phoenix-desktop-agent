// Package config reads and writes Phoenix's config.yaml.
//
// config.yaml can name programs to execute (a provider's cli_path, later MCP server
// commands), so it is a code-execution surface. Agent tools must never read or write it,
// and changes to it that arrive from the UI are validated here.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"phoenix/internal/mcp"
	"phoenix/internal/provider"
	"phoenix/internal/workspace"
)

// ProviderConfig is one provider's settings.
type ProviderConfig struct {
	Auth    provider.AuthMode `yaml:"auth"`               // cli | api_key
	APIKey  string            `yaml:"api_key,omitempty"`  // vault:<name>, env:<NAME>, or a literal
	Model   string            `yaml:"model,omitempty"`    // optional default model
	Effort  string            `yaml:"effort,omitempty"`   // optional reasoning effort
	CLIPath string            `yaml:"cli_path,omitempty"` // optional; searched for when empty
	BaseURL string            `yaml:"base_url,omitempty"` // HTTP providers; empty means the provider's default
}

// Browser is the browser section.
type Browser struct {
	// SearchURL is what the address bar uses for text that is not a URL. It must be https
	// and contain {query}.
	SearchURL string `yaml:"search_url,omitempty"`
}

// DefaultSearchURL is used when none is configured.
const DefaultSearchURL = "https://duckduckgo.com/?q={query}"

// ValidSearchURL reports whether s is an acceptable search template: https, a host, and
// exactly one {query} placeholder. Anything else could send what the user types to a
// scheme or host they did not choose.
func ValidSearchURL(s string) bool {
	if len(s) > 300 || strings.Count(s, "{query}") != 1 {
		return false
	}
	u, err := url.Parse(strings.Replace(s, "{query}", "x", 1))
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

// Desktop is the computer-use section: whether the agent may be offered the desktop tools
// (screenshot, click, scroll, drag, copy, paste, ...).
type Desktop struct {
	Enabled bool `yaml:"enabled"`
	// CLIPath is retained for compatibility with older configuration files; it is ignored.
	CLIPath string `yaml:"cli_path,omitempty"`
}

// Workspace is the folder the agent stores files in. Empty means Documents\Phoenix.
type Workspace struct {
	Dir string `yaml:"dir,omitempty"`
}

// Providers is the providers section.
type Providers struct {
	Default string         `yaml:"default,omitempty"`
	Claude  ProviderConfig `yaml:"claude"`
	Codex   ProviderConfig `yaml:"codex"`
	// Others holds the HTTP providers (gemini, openrouter, mistral, zai, grok) by id.
	Others map[string]ProviderConfig `yaml:",inline"`
}

// Config is the whole file. Sections Phoenix does not know yet are kept in Rest so a
// rewrite does not drop them. Comments are not preserved.
type Config struct {
	Providers Providers `yaml:"providers"`
	Browser   Browser   `yaml:"browser,omitempty"`
	Desktop   Desktop   `yaml:"desktop,omitempty"`
	Workspace Workspace `yaml:"workspace,omitempty"`
	// MCPServers are the installed MCP servers by name. Entries run programs or call URLs, so
	// they are validated on load and only the Settings UI may change them.
	MCPServers map[string]mcp.Server `yaml:"mcp_servers,omitempty"`
	Rest       map[string]any        `yaml:",inline"`
}

// SearchURL is the configured search template, or the default.
func (c *Config) SearchURL() string {
	if ValidSearchURL(c.Browser.SearchURL) {
		return c.Browser.SearchURL
	}
	return DefaultSearchURL
}

// Store is a config file shared by everything that edits it. All access goes through the
// mutex, and every Update is written to disk before it returns.
type Store struct {
	mu   sync.Mutex
	path string
	cfg  *Config
}

// OpenStore loads path (defaults when missing).
func OpenStore(path string) (*Store, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, cfg: cfg}, nil
}

// Path is the file the store writes.
func (s *Store) Path() string { return s.path }

// View calls f with the config held; f must not keep the pointer.
func (s *Store) View(f func(*Config)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s.cfg)
}

// Update calls f with the config held and then saves it. On a save error the in-memory
// change is rolled back so memory and disk agree.
func (s *Store) Update(f func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := yaml.Marshal(s.cfg)
	if err != nil {
		return err
	}
	f(s.cfg)
	if err := s.cfg.Save(s.path); err != nil {
		_ = yaml.Unmarshal(before, s.cfg) // best effort
		return err
	}
	return nil
}

// DefaultPath is %APPDATA%\Phoenix\config.yaml.
func DefaultPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", errors.New("APPDATA is not set")
	}
	return filepath.Join(appData, "Phoenix", "config.yaml"), nil
}

// Load reads path. A missing file yields the defaults.
func Load(path string) (*Config, error) {
	c := &Config{Desktop: Desktop{Enabled: true}}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := yaml.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	c.normalize()
	return c, nil
}

// normalize fills defaults and drops invalid values so the rest of the app can trust them.
func (c *Config) normalize() {
	for _, p := range []*ProviderConfig{&c.Providers.Claude, &c.Providers.Codex} {
		if !p.Auth.Valid() {
			p.Auth = provider.AuthCLI
		}
	}
	// Anything under providers: that is not an HTTP provider Phoenix knows is dropped, so a
	// stray key can neither be used nor grow the file.
	for k, p := range c.Providers.Others {
		if !provider.IsHTTP(provider.ID(k)) {
			delete(c.Providers.Others, k)
			continue
		}
		p.Auth = provider.AuthAPIKey // HTTP providers have no other way in
		c.Providers.Others[k] = p
	}
	if d := provider.ID(c.Providers.Default); !provider.Known(d) {
		c.Providers.Default = string(provider.Claude)
	}
	cleanProvider(&c.Providers.Claude)
	cleanProvider(&c.Providers.Codex)
	for k, p := range c.Providers.Others {
		cleanProvider(&p)
		c.Providers.Others[k] = p
	}
	if c.Browser.SearchURL != "" && !ValidSearchURL(c.Browser.SearchURL) {
		c.Browser.SearchURL = ""
	}
	c.MCPServers = mcp.CleanMap(c.MCPServers)
	if d := c.Workspace.Dir; d != "" && (!filepath.IsAbs(d) || workspace.ValidateLocation(d) != nil) {
		c.Workspace.Dir = ""
	}
	if p := c.Desktop.CLIPath; p != "" && (!filepath.IsAbs(p) || !strings.HasSuffix(strings.ToLower(p), ".exe")) {
		c.Desktop.CLIPath = ""
	}
}

// cleanProvider drops values that cannot be trusted. The model and effort become
// command-line arguments or request fields, and the base URL receives the API key, so it
// must be a plain https origin.
func cleanProvider(p *ProviderConfig) {
	if !provider.ValidModelID(p.Model) {
		p.Model = ""
	}
	if !provider.ValidEffort(p.Effort) {
		p.Effort = ""
	}
	if !provider.ValidBaseURL(p.BaseURL) {
		p.BaseURL = ""
	}
}

// Provider returns a provider's settings. An HTTP provider with nothing saved gets the
// API-key default; an unknown id gets the CLI-login default.
func (c *Config) Provider(id provider.ID) ProviderConfig {
	switch id {
	case provider.Claude:
		return c.Providers.Claude
	case provider.Codex:
		return c.Providers.Codex
	}
	if provider.IsHTTP(id) {
		if p, ok := c.Providers.Others[string(id)]; ok {
			return p
		}
		return ProviderConfig{Auth: provider.AuthAPIKey}
	}
	return ProviderConfig{Auth: provider.AuthCLI}
}

// SetProvider replaces a known provider's settings. Unknown ids are ignored.
func (c *Config) SetProvider(id provider.ID, p ProviderConfig) {
	switch {
	case id == provider.Claude:
		c.Providers.Claude = p
	case id == provider.Codex:
		c.Providers.Codex = p
	case provider.IsHTTP(id):
		if c.Providers.Others == nil {
			c.Providers.Others = map[string]ProviderConfig{}
		}
		c.Providers.Others[string(id)] = p
	}
	c.normalize()
}

// SetDefault chooses the provider used when none is named. Unknown ids are ignored.
func (c *Config) SetDefault(id provider.ID) {
	if provider.Known(id) {
		c.Providers.Default = string(id)
	}
}

// Save writes the file atomically with owner-only permissions.
func (c *Config) Save(path string) error {
	c.normalize()
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	header := "# Phoenix configuration. API keys should be vault:<name> or env:<NAME> references.\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(header), b...), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// VaultName is the vault entry used for a provider's API key.
func VaultName(id provider.ID) string { return "provider-" + string(id) }

// KeyRef is the reference written to config.yaml for a key kept in the vault.
func KeyRef(id provider.ID) string { return "vault:" + VaultName(id) }

// IsLiteralKey reports whether a key field holds the key itself rather than a reference.
func IsLiteralKey(ref string) bool {
	ref = strings.TrimSpace(ref)
	return ref != "" && !strings.HasPrefix(ref, "vault:") && !strings.HasPrefix(ref, "env:")
}
