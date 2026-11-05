package providerhub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

// MCPUpdate replaces a server's connection settings. Blank values keep existing secret
// references; omitted keys remove them. New values are stored under fresh vault names so a
// failed config save cannot overwrite credentials that are still in use.
func (h *Hub) MCPUpdate(s mcp.Server, env, headers map[string]string) error {
	s.Name = strings.TrimSpace(s.Name)
	if mcp.Reserved(s.Name) {
		return errors.New("names starting with phoenix- are reserved")
	}
	var old mcp.Server
	var exists bool
	h.store.View(func(c *config.Config) { old, exists = c.MCPServers[s.Name] })
	if !exists {
		return errors.New("no such server")
	}
	if s.Transport != old.Transport {
		return errors.New("changing a server's transport requires removing and adding it again")
	}
	s.Enabled = old.Enabled
	if s.Transport == mcp.HTTP && s.URL == old.URL {
		s.OAuthRef = old.OAuthRef
	}
	s.Env, s.Headers = nil, nil
	if err := s.Validate(); err != nil {
		return err
	}

	var created []string
	undo := func() {
		for _, name := range created {
			_ = h.vault.Delete(name)
		}
	}
	put := func(kind string, values, previous map[string]string) (map[string]string, error) {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		refs := make(map[string]string, len(keys))
		for i, key := range keys {
			value := values[key]
			if value == "" {
				if ref, ok := previous[key]; ok {
					refs[key] = ref
				}
				continue
			}
			name, err := h.newMCPSecretName(s.Name, kind, i)
			if err != nil {
				return nil, err
			}
			if err := h.vault.Set(name, value); err != nil {
				return nil, err
			}
			created = append(created, name)
			refs[key] = "vault:" + name
		}
		if len(refs) == 0 {
			return nil, nil
		}
		return refs, nil
	}

	var err error
	if s.Env, err = put("e", env, old.Env); err != nil {
		undo()
		return err
	}
	if s.Headers, err = put("h", headers, old.Headers); err != nil {
		undo()
		return err
	}
	if err := s.Validate(); err != nil {
		undo()
		return err
	}
	saved := false
	err = h.store.Update(func(c *config.Config) {
		current, ok := c.MCPServers[s.Name]
		if !ok || !reflect.DeepEqual(current, old) {
			return
		}
		c.MCPServers[s.Name] = s
		saved = true
	})
	if err != nil {
		undo()
		return err
	}
	if !saved {
		undo()
		return errors.New("server settings changed while the edit was being saved; reopen the server and try again")
	}

	retained := map[string]bool{}
	for _, refs := range []map[string]string{s.Env, s.Headers} {
		for _, ref := range refs {
			retained[ref] = true
		}
	}
	prefix := "vault:mcp-" + s.Name + "-"
	for _, refs := range []map[string]string{old.Env, old.Headers} {
		for _, ref := range refs {
			if strings.HasPrefix(ref, prefix) && !retained[ref] {
				_ = h.vault.Delete(strings.TrimPrefix(ref, "vault:"))
			}
		}
	}
	if old.OAuthRef != "" && old.OAuthRef != s.OAuthRef {
		_ = h.vault.Delete(strings.TrimPrefix(old.OAuthRef, "vault:"))
	}
	return nil
}

func (h *Hub) newMCPSecretName(server, kind string, i int) (string, error) {
	for range 4 {
		var nonce [8]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", err
		}
		name := fmt.Sprintf("mcp-%s-%s%d-%s", server, kind, i, hex.EncodeToString(nonce[:]))
		if !h.vault.Has(name) {
			return name, nil
		}
	}
	return "", errors.New("could not create a unique vault entry")
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
	if s.OAuthRef != "" {
		_ = h.vault.Delete(strings.TrimPrefix(s.OAuthRef, "vault:"))
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
	resolved, err := mcp.Resolve(s, func(ref string) (string, error) {
		v, _, err := h.vault.Resolve(ref)
		return v, err
	})
	if err != nil {
		return mcp.Resolved{}, err
	}
	if s.OAuthRef != "" {
		raw, _, err := h.vault.Resolve(s.OAuthRef)
		if err != nil {
			return mcp.Resolved{}, err
		}
		if raw != "" {
			var record mcp.OAuthRecord
			if err := json.Unmarshal([]byte(raw), &record); err != nil || record.Token == nil || record.ClientID == "" {
				return mcp.Resolved{}, errors.New("saved OAuth credentials are invalid; remove and reconnect this server")
			}
			resolved.OAuthRecord = &record
		}
	}
	resolved.SaveOAuthRecord = func(record mcp.OAuthRecord) error {
		return h.saveMCPOAuth(s.Name, s.URL, record)
	}
	return resolved, nil
}

func (h *Hub) saveMCPOAuth(name, endpoint string, record mcp.OAuthRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	secretName, err := h.newMCPSecretName(name, "oauth", 0)
	if err != nil {
		return err
	}
	if err := h.vault.Set(secretName, string(raw)); err != nil {
		return err
	}
	newRef := "vault:" + secretName
	oldRef := ""
	saved := false
	err = h.store.Update(func(c *config.Config) {
		server, ok := c.MCPServers[name]
		if !ok || server.Transport != mcp.HTTP || server.URL != endpoint {
			return
		}
		oldRef = server.OAuthRef
		server.OAuthRef = newRef
		c.MCPServers[name] = server
		saved = true
	})
	if err != nil || !saved {
		_ = h.vault.Delete(secretName)
		if err != nil {
			return err
		}
		return errors.New("the server changed during sign-in; reopen it and try again")
	}
	if oldRef != "" && oldRef != newRef {
		_ = h.vault.Delete(strings.TrimPrefix(oldRef, "vault:"))
	}
	return nil
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

// attachClaudeProxy gives the isolated Claude CLI a private, per-run proxy config.
func attachClaudeProxy(req *provider.Request, proxy *codexToolProxy) (func(), error) {
	server := mcp.Resolved{Server: mcp.Server{Name: "phoenix-tools", Transport: mcp.HTTP, URL: proxy.url, Enabled: true}, HeaderValues: map[string]string{"Authorization": "Bearer " + proxy.token}}
	raw, err := mcp.ClaudeConfig([]mcp.Resolved{server})
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "phoenix-mcp-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		cleanup()
		return nil, err
	}
	req.MCPConfig = path
	req.AllowedTools = append(req.AllowedTools, "mcp__phoenix-tools")
	return cleanup, nil
}

// Desktop reports whether computer use is on and the legacy configured CLI path.
func (h *Hub) Desktop() (enabled bool, cliPath string) {
	h.store.View(func(c *config.Config) { enabled, cliPath = c.Desktop.Enabled, c.Desktop.CLIPath })
	return
}

// WorkspaceDir is the configured file workspace, or "" for the default.
func (h *Hub) WorkspaceDir() (dir string) {
	h.store.View(func(c *config.Config) { dir = c.Workspace.Dir })
	return
}

// SetWorkspaceDir changes the file workspace; "" restores the default.
func (h *Hub) SetWorkspaceDir(dir string) error {
	return h.store.Update(func(c *config.Config) { c.Workspace.Dir = dir })
}

// SetDesktopEnabled turns computer use on or off.
func (h *Hub) SetDesktopEnabled(on bool) error {
	return h.store.Update(func(c *config.Config) { c.Desktop.Enabled = on })
}
