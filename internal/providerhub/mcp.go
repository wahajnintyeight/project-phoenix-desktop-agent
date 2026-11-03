package providerhub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"phoenix/internal/config"
	"phoenix/internal/mcp"
	"phoenix/internal/provider"
)

// MCP servers: install, remove, enable, test, and attach the enabled ones to a run. Secret
// values (environment variables and headers) go to the vault; config.yaml keeps references.

// MCPServers lists the installed servers.
func (h *Hub) MCPServers() []mcp.Server {
	var out []mcp.Server
	h.store.View(func(c *config.Config) { out = mcp.List(c.MCPServers) })
	return out
}

// MCPEnabled lists the servers offered to the agent.
func (h *Hub) MCPEnabled() []mcp.Server {
	var out []mcp.Server
	for _, s := range h.MCPServers() {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

func mcpSecretName(server, kind string, i int) string {
	return fmt.Sprintf("mcp-%s-%s%d", server, kind, i)
}

// MCPAdd installs a server. env and headers hold the secret values: each is stored in the
// vault and only a reference is saved. The new server starts disabled until the user turns
// it on, so installing never exposes its tools by itself. An existing name is refused;
// remove it first.
func (h *Hub) MCPAdd(s mcp.Server, env, headers map[string]string) error {
	s.Name = strings.TrimSpace(s.Name)
	if mcp.Reserved(s.Name) {
		return errors.New("names starting with phoenix- are reserved")
	}
	s.Enabled = false
	s.Env, s.Headers = nil, nil
	if err := s.Validate(); err != nil {
		return err
	}
	exists := false
	h.store.View(func(c *config.Config) { _, exists = c.MCPServers[s.Name] })
	if exists {
		return errors.New("a server with that name is already installed")
	}
	var stored []string
	undo := func() {
		for _, n := range stored {
			_ = h.vault.Delete(n)
		}
	}
	put := func(kind string, vals map[string]string) (map[string]string, error) {
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		refs := map[string]string{}
		for i, k := range keys {
			if vals[k] == "" {
				continue
			}
			name := mcpSecretName(s.Name, kind, i)
			if err := h.vault.Set(name, vals[k]); err != nil {
				return nil, err
			}
			stored = append(stored, name)
			refs[k] = "vault:" + name
		}
		if len(refs) == 0 {
			return nil, nil
		}
		return refs, nil
	}
	var err error
	if s.Env, err = put("e", env); err != nil {
		undo()
		return err
	}
	if s.Headers, err = put("h", headers); err != nil {
		undo()
		return err
	}
	if err := s.Validate(); err != nil { // re-check with the references filled in
		undo()
		return err
	}
	if err := h.store.Update(func(c *config.Config) {
		if c.MCPServers == nil {
			c.MCPServers = map[string]mcp.Server{}
		}
		c.MCPServers[s.Name] = s
	}); err != nil {
		undo()
		return err
	}
	return nil
}

// MCPRemove uninstalls a server and deletes the vault entries it owned.
func (h *Hub) MCPRemove(name string) error {
	var s mcp.Server
	var ok bool
	h.store.View(func(c *config.Config) { s, ok = c.MCPServers[name] })
	if !ok {
		return nil
	}
	if err := h.store.Update(func(c *config.Config) { delete(c.MCPServers, name) }); err != nil {
		return err
	}
	prefix := "vault:mcp-" + name + "-"
	for _, m := range []map[string]string{s.Env, s.Headers} {
		for _, ref := range m {
			if strings.HasPrefix(ref, prefix) {
				_ = h.vault.Delete(strings.TrimPrefix(ref, "vault:"))
			}
		}
	}
	return nil
}

// MCPSetEnabled turns a server on or off for the agent.
func (h *Hub) MCPSetEnabled(name string, on bool) error {
	var found bool
	err := h.store.Update(func(c *config.Config) {
		if s, ok := c.MCPServers[name]; ok {
			found = true
			s.Enabled = on
			c.MCPServers[name] = s
		}
	})
	if err == nil && !found {
		return errors.New("no such server")
	}
	return err
}

func (h *Hub) resolveMCP(s mcp.Server) (mcp.Resolved, error) {
	return mcp.Resolve(s, func(ref string) (string, error) {
		v, _, err := h.vault.Resolve(ref)
		return v, err
	})
}

// MCPTest connects to an installed server (starting it if it is a program) and lists its
// tools. This runs the configured command, so it is only called from the Test button.
func (h *Hub) MCPTest(ctx context.Context, name string) (mcp.Info, error) {
	var s mcp.Server
	var ok bool
	h.store.View(func(c *config.Config) { s, ok = c.MCPServers[name] })
	if !ok {
		return mcp.Info{}, errors.New("no such server")
	}
	r, err := h.resolveMCP(s)
	if err != nil {
		return mcp.Info{}, err
	}
	return mcp.Test(ctx, r)
}

// attachMCP points the request at a private config file for the enabled servers. The file
// holds secret values, so it lives in its own directory, is owner-only, and is removed when
// the run ends.
func (h *Hub) attachMCP(req *provider.Request) (cleanup func(), err error) {
	servers := h.MCPEnabled()
	res := make([]mcp.Resolved, 0, len(servers)+len(req.MCPServers))
	for _, s := range servers {
		r, err := h.resolveMCP(s)
		if err != nil {
			return nil, fmt.Errorf("MCP server %s: %w", s.Name, err)
		}
		res = append(res, r)
	}
	res = append(res, req.MCPServers...)
	if len(res) == 0 {
		return func() {}, nil
	}
	raw, err := mcp.ClaudeConfig(res)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "phoenix-mcp-")
	if err != nil {
		return nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		cleanup()
		return nil, err
	}
	req.MCPConfig = path
	req.AllowedTools = append(req.AllowedTools, mcp.ClaudeAllowed(res)...)
	return cleanup, nil
}

// Desktop reports whether computer use is on and where the agent-desktop program was
// configured to be ("" means search for it).
func (h *Hub) Desktop() (enabled bool, cliPath string) {
	h.store.View(func(c *config.Config) { enabled, cliPath = c.Desktop.Enabled, c.Desktop.CLIPath })
	return
}

// SetDesktopEnabled turns computer use on or off.
func (h *Hub) SetDesktopEnabled(on bool) error {
	return h.store.Update(func(c *config.Config) { c.Desktop.Enabled = on })
}
