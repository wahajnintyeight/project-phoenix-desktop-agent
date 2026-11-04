// Package providerhub is the one place that joins provider settings (config.yaml), API keys
// (the vault) and the vendor CLIs. The UI and, later, the agent runtime talk to the hub.
package providerhub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"phoenix/internal/config"
	"phoenix/internal/mcp"
	"phoenix/internal/provider"
	"phoenix/internal/secrets"
)

// Hub resolves provider settings and keys on demand. The config file is shared with the
// rest of the app through a config.Store.
type Hub struct {
	store     *config.Store
	vault     *secrets.Store
	providers map[provider.ID]provider.Provider

	modelsMu sync.Mutex
	models   map[provider.ID]modelList
}

type modelList struct {
	sig   string // what the list was fetched with: mode, base URL, whether a key was present
	items []provider.Model
}

// New loads (or defaults) the config at path and uses vault for keys.
func New(path string, vault *secrets.Store, providers ...provider.Provider) (*Hub, error) {
	store, err := config.OpenStore(path)
	if err != nil {
		return nil, err
	}
	return NewWithStore(store, vault, providers...), nil
}

// NewWithStore is New for a config store that other parts of the app also use.
func NewWithStore(store *config.Store, vault *secrets.Store, providers ...provider.Provider) *Hub {
	if len(providers) == 0 {
		providers = append([]provider.Provider{provider.ClaudeProvider{}, provider.CodexProvider{}}, provider.NewHTTPProviders()...)
	}
	h := &Hub{
		store: store, vault: vault,
		providers: map[provider.ID]provider.Provider{},
		models:    map[provider.ID]modelList{},
	}
	for _, p := range providers {
		h.providers[p.ID()] = p
	}
	return h
}

// IDs lists the known providers in display order.
func (h *Hub) IDs() []provider.ID { return provider.AllIDs() }

func (h *Hub) get(id provider.ID) (provider.Provider, error) {
	p, ok := h.providers[id]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", id)
	}
	return p, nil
}

// resolved is a provider's settings plus where its key came from.
type resolved struct {
	provider.Settings
	keySource secrets.RefKind
	literal   bool
}

func (h *Hub) resolve(id provider.ID) (resolved, error) {
	var pc config.ProviderConfig
	h.store.View(func(c *config.Config) { pc = c.Provider(id) })
	key, kind, err := h.vault.Resolve(pc.APIKey)
	if err != nil {
		return resolved{}, err
	}
	return resolved{
		Settings: provider.Settings{
			Mode: pc.Auth, APIKey: key, Model: pc.Model, Effort: pc.Effort, CLIPath: pc.CLIPath, BaseURL: pc.BaseURL,
		},
		keySource: kind,
		literal:   config.IsLiteralKey(pc.APIKey),
	}, nil
}

// Status detects one provider. The key itself is never part of the result.
func (h *Hub) Status(ctx context.Context, id provider.ID) provider.Status {
	p, err := h.get(id)
	if err != nil {
		return provider.Status{ID: id, Problem: err.Error()}
	}
	r, err := h.resolve(id)
	if err != nil {
		return provider.Status{ID: id, Name: string(id), Problem: err.Error()}
	}
	st := p.Detect(ctx, r.Settings)
	st.KeySource = string(r.keySource)
	return st
}

// Statuses detects every provider concurrently, in display order.
func (h *Hub) Statuses(ctx context.Context) []provider.Status {
	ids := h.IDs()
	out := make([]provider.Status, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = h.Status(ctx, id)
		}()
	}
	wg.Wait()
	return out
}

// Default is the provider used when none is named.
func (h *Hub) Default() provider.ID {
	var d string
	h.store.View(func(c *config.Config) { d = c.Providers.Default })
	return provider.ID(d)
}

func (h *Hub) update(id provider.ID, mutate func(*config.ProviderConfig)) error {
	if _, err := h.get(id); err != nil {
		return err
	}
	return h.store.Update(func(c *config.Config) {
		pc := c.Provider(id)
		mutate(&pc)
		c.SetProvider(id, pc)
	})
}

// SetMode chooses CLI sign-in or API key for a provider.
func (h *Hub) SetMode(id provider.ID, mode provider.AuthMode) error {
	if !mode.Valid() {
		return fmt.Errorf("invalid auth mode %q", mode)
	}
	if provider.IsHTTP(id) && mode != provider.AuthAPIKey {
		return errors.New("this provider only works with an API key")
	}
	return h.update(id, func(pc *config.ProviderConfig) { pc.Auth = mode })
}

// SetAPIKey stores key in the vault and writes only a reference to config.yaml.
func (h *Hub) SetAPIKey(id provider.ID, key string) error {
	if _, err := h.get(id); err != nil {
		return err
	}
	if len(key) < 8 || len(key) > 512 {
		return errors.New("that does not look like an API key")
	}
	if err := h.vault.Set(config.VaultName(id), key); err != nil {
		return err
	}
	h.forgetModels(id)
	return h.update(id, func(pc *config.ProviderConfig) { pc.APIKey = config.KeyRef(id) })
}

// ClearAPIKey removes the vault entry and the reference, and returns to CLI sign-in if
// the provider was set to use the key.
func (h *Hub) ClearAPIKey(id provider.ID) error {
	if _, err := h.get(id); err != nil {
		return err
	}
	if err := h.vault.Delete(config.VaultName(id)); err != nil {
		return err
	}
	h.forgetModels(id)
	return h.update(id, func(pc *config.ProviderConfig) {
		pc.APIKey = ""
		if pc.Auth == provider.AuthAPIKey && !provider.IsHTTP(id) {
			pc.Auth = provider.AuthCLI
		}
	})
}

// SetBaseURL points an HTTP provider at another endpoint ("" restores the default). The API
// key is sent there, so only a plain https origin (or http on this machine) is accepted.
func (h *Hub) SetBaseURL(id provider.ID, base string) error {
	if !provider.IsHTTP(id) {
		return errors.New("this provider has no base URL")
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if !provider.ValidBaseURL(base) {
		return errors.New("the address must start with https:// and have no login, query or fragment (http is allowed only for localhost)")
	}
	h.forgetModels(id)
	return h.update(id, func(pc *config.ProviderConfig) { pc.BaseURL = base })
}

func (h *Hub) forgetModels(id provider.ID) {
	h.modelsMu.Lock()
	delete(h.models, id)
	h.modelsMu.Unlock()
}

// SetDefault chooses the default provider.
func (h *Hub) SetDefault(id provider.ID) error {
	if _, err := h.get(id); err != nil {
		return err
	}
	return h.store.Update(func(c *config.Config) { c.SetDefault(id) })
}

// Choice is a provider's selected model and effort ("" means the CLI/model default).
type Choice struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// Selected reports the saved model choice.
func (h *Hub) Selected(id provider.ID) Choice {
	var c Choice
	h.store.View(func(cfg *config.Config) {
		pc := cfg.Provider(id)
		c = Choice{Model: pc.Model, Effort: pc.Effort}
	})
	return c
}

// SetModel saves the model and effort used for runs. Both are checked because they become
// command-line arguments. Any valid model id is allowed, not only listed ones, so a model
// newer than the CLI's list can be used.
func (h *Hub) SetModel(id provider.ID, model, effort string) error {
	if _, err := h.get(id); err != nil {
		return err
	}
	if !provider.ValidModelID(model) {
		return provider.ErrBadModel
	}
	if !provider.ValidEffort(effort) {
		return errors.New("that is not a valid effort level")
	}
	return h.update(id, func(pc *config.ProviderConfig) { pc.Model, pc.Effort = model, effort })
}

// Models lists the models a provider offers. The answer is cached per auth mode; refresh
// forces the CLI to be asked again (Codex's catalog changes with its version).
func (h *Hub) Models(ctx context.Context, id provider.ID, refresh bool) ([]provider.Model, error) {
	p, err := h.get(id)
	if err != nil {
		return nil, err
	}
	r, err := h.resolve(id)
	if err != nil {
		return nil, err
	}
	h.modelsMu.Lock()
	cached, ok := h.models[id]
	h.modelsMu.Unlock()
	sig := fmt.Sprintf("%s|%s|%t", r.Mode, r.BaseURL, r.APIKey != "")
	if ok && !refresh && cached.sig == sig {
		return cached.items, nil
	}
	items, err := p.Models(ctx, r.Settings)
	if err != nil {
		return nil, err
	}
	h.modelsMu.Lock()
	h.models[id] = modelList{sig: sig, items: items}
	h.modelsMu.Unlock()
	return items, nil
}

// Login runs the vendor CLI's own sign-in in a console window.
func (h *Hub) Login(ctx context.Context, id provider.ID) error {
	p, err := h.get(id)
	if err != nil {
		return err
	}
	r, err := h.resolve(id)
	if err != nil {
		return err
	}
	return p.Login(ctx, r.Settings)
}

// Run streams a prompt through a provider.
func (h *Hub) Run(ctx context.Context, id provider.ID, req provider.Request, emit func(provider.Event)) (provider.Result, error) {
	p, err := h.get(id)
	if err != nil {
		return provider.Result{}, err
	}
	r, err := h.resolve(id)
	if err != nil {
		return provider.Result{}, err
	}
	if (req.UseMCP || len(req.MCPServers) > 0) && id == provider.Claude && req.MCPConfig == "" {
		cleanup, err := h.attachMCP(&req)
		if err != nil {
			return provider.Result{}, err
		}
		defer cleanup()
	}
	if provider.IsHTTP(id) && (req.UseMCP || len(req.MCPServers) > 0) {
		servers := make([]mcp.Resolved, 0, len(req.MCPServers)+len(h.MCPEnabled()))
		for _, server := range h.MCPEnabled() {
			resolved, err := h.resolveMCP(server)
			if err != nil {
				return provider.Result{}, fmt.Errorf("MCP server %s: %w", server.Name, err)
			}
			resolved.RequireApproval = true
			servers = append(servers, resolved)
		}
		servers = append(servers, req.MCPServers...)
		if len(servers) > 0 {
			runtime, err := mcp.Open(ctx, servers)
			if err != nil {
				return provider.Result{}, err
			}
			defer runtime.Close()
			for _, function := range runtime.Functions() {
				req.Tools = append(req.Tools, provider.Tool{
					Name: function.Name, Server: function.Server, MCPName: function.MCPName,
					Description: function.Description, Parameters: function.Parameters,
					RequiresApproval: function.RequireApproval,
				})
			}
			req.ExecuteTool = func(callCtx context.Context, call provider.ToolCall) (provider.ToolOutput, error) {
				result, err := runtime.Call(callCtx, call.Tool.Name, call.Arguments)
				if errors.Is(err, mcp.ErrUserDeclined) {
					return provider.ToolOutput{}, provider.ErrToolDenied
				}
				out := provider.ToolOutput{Text: result.Text}
				for _, image := range result.Images {
					out.Images = append(out.Images, provider.ToolImage{MIMEType: image.MIMEType, Data: image.Data})
				}
				return out, err
			}
		}
	}
	return p.Run(ctx, r.Settings, req, emit)
}

// Test sends a one-word prompt to prove the provider works end to end in its current mode.
func (h *Hub) Test(ctx context.Context, id provider.ID) (provider.Result, error) {
	return h.Run(ctx, id, provider.Request{Prompt: "Reply with exactly the word: ok"}, func(provider.Event) {})
}
