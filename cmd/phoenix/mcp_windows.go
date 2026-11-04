package main

import (
	"context"
	"errors"
	"sort"
	"time"

	"phoenix/internal/mcp"
)

// MCP servers in Settings. The UI sends text; Go parses, validates and stages it, shows the
// exact command back, and installs only when the user confirms. What is staged lives here, so
// the UI cannot change it between the preview and the confirmation. State is touched on the
// UI thread only; tests run on a goroutine and report back through runLater.

var (
	mcpStaged       []mcp.Candidate
	mcpStagedUpdate string
	mcpTestCancel   = map[string]context.CancelFunc{}
)

type mcpServerView struct {
	Name       string   `json:"name"`
	Transport  string   `json:"transport"`
	Summary    string   `json:"summary"`
	Command    string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
	URL        string   `json:"url,omitempty"`
	Enabled    bool     `json:"enabled"`
	Authorized bool     `json:"authorized,omitempty"`
	Env        []string `json:"env,omitempty"`
	Headers    []string `json:"headers,omitempty"`
}

type mcpPendingView struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Summary   string   `json:"summary"`
	Env       []string `json:"env,omitempty"`
	Headers   []string `json:"headers,omitempty"`
}

type mcpData struct {
	Servers   []mcpServerView  `json:"servers"`
	Pending   []mcpPendingView `json:"pending,omitempty"`
	Updating  bool             `json:"updating,omitempty"`
	Error     string           `json:"error,omitempty"`
	Installed bool             `json:"installed,omitempty"`
}

type mcpTestView struct {
	Name     string     `json:"name"`
	OK       bool       `json:"ok"`
	Canceled bool       `json:"canceled,omitempty"`
	Error    string     `json:"error,omitempty"`
	Server   string     `json:"server,omitempty"`
	Version  string     `json:"version,omitempty"`
	Tools    []mcp.Tool `json:"tools,omitempty"`
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// pushMCP sends the installed servers and whatever is staged for confirmation.
func pushMCP(errMsg string, installed bool) {
	if hub == nil {
		return
	}
	d := mcpData{Servers: []mcpServerView{}, Error: errMsg, Installed: installed, Updating: mcpStagedUpdate != ""}
	for _, s := range hub.MCPServers() {
		d.Servers = append(d.Servers, mcpServerView{
			Name: s.Name, Transport: string(s.Transport), Summary: s.Describe(), Enabled: s.Enabled,
			Command: s.Command, Args: append([]string(nil), s.Args...), URL: s.URL,
			Authorized: s.OAuthRef != "",
			Env:        sortedKeys(s.Env), Headers: sortedKeys(s.Headers),
		})
	}
	for _, c := range mcpStaged {
		d.Pending = append(d.Pending, mcpPendingView{
			Name: c.Name, Transport: string(c.Transport), Summary: c.Describe(),
			Env: sortedKeys(c.EnvValues), Headers: sortedKeys(c.HeaderValues),
		})
	}
	post("u" + fs + "mcp-servers" + fs + "data" + fs + jsonString(d))
}

// handleMCPOp handles UI to Go "v mcp-*" messages. The staged JSON can carry secret values,
// so nothing from it is logged.
func handleMCPOp(f []string) {
	if hub == nil {
		return
	}
	arg := ""
	if len(f) > 2 {
		arg = f[2]
	}
	switch f[1] {
	case "mcp-stage":
		cands, err := mcp.ParseImport(arg)
		if err != nil {
			mcpStaged = nil
			mcpStagedUpdate = ""
			pushMCP(err.Error(), false)
			return
		}
		// Refuse names already taken now, rather than after the user confirms.
		taken := map[string]bool{}
		for _, s := range hub.MCPServers() {
			taken[s.Name] = true
		}
		for _, c := range cands {
			if taken[c.Name] {
				mcpStaged = nil
				mcpStagedUpdate = ""
				pushMCP("A server named "+c.Name+" is already installed. Remove it first, or choose another name.", false)
				return
			}
		}
		mcpStaged = cands
		mcpStagedUpdate = ""
		pushMCP("", false)
	case "mcp-update-stage":
		if len(f) < 4 {
			return
		}
		name := arg
		cands, err := mcp.ParseImport(f[3])
		if err != nil {
			mcpStaged = nil
			mcpStagedUpdate = ""
			pushMCP(err.Error(), false)
			return
		}
		if len(cands) != 1 || cands[0].Name != name {
			mcpStaged = nil
			mcpStagedUpdate = ""
			pushMCP("Edit one server at a time, keeping its current name.", false)
			return
		}
		if !containsMCPServer(name) {
			mcpStaged = nil
			mcpStagedUpdate = ""
			pushMCP("That server is no longer installed.", false)
			return
		}
		mcpStaged = cands
		mcpStagedUpdate = name
		pushMCP("", false)
	case "mcp-cancel":
		mcpStaged = nil
		mcpStagedUpdate = ""
		pushMCP("", false)
	case "mcp-confirm":
		staged := mcpStaged
		updating := mcpStagedUpdate
		mcpStaged = nil
		mcpStagedUpdate = ""
		if updating != "" {
			if len(staged) != 1 || staged[0].Name != updating {
				pushMCP("The staged server changes are no longer valid.", false)
				return
			}
			c := staged[0]
			if err := hub.MCPUpdate(c.Server, c.EnvValues, c.HeaderValues); err != nil {
				logf("mcp update %s: %v", c.Name, err)
				pushMCP("Could not update "+c.Name+": "+err.Error(), false)
				return
			}
			logf("mcp updated %s", c.Name)
		} else {
			for _, c := range staged {
				if err := hub.MCPAdd(c.Server, c.EnvValues, c.HeaderValues); err != nil {
					logf("mcp install %s: %v", c.Name, err)
					pushMCP("Could not install "+c.Name+": "+err.Error(), false)
					return
				}
				logf("mcp installed %s", c.Name)
			}
		}
		pushMCP("", len(staged) > 0)
	case "mcp-remove":
		if cancel, ok := mcpTestCancel[arg]; ok {
			cancel()
		}
		if err := hub.MCPRemove(arg); err != nil {
			logf("mcp remove %s: %v", arg, err)
			pushMCP("Could not remove "+arg+": "+err.Error(), false)
			return
		}
		pushMCP("", false)
	case "mcp-enable":
		on := len(f) > 3 && f[3] == "1"
		if err := hub.MCPSetEnabled(arg, on); err != nil {
			pushMCP(err.Error(), false)
			return
		}
		pushMCP("", false)
	case "mcp-test":
		startMCPTest(arg)
	}
}

func containsMCPServer(name string) bool {
	for _, server := range hub.MCPServers() {
		if server.Name == name {
			return true
		}
	}
	return false
}

func startMCPTest(name string) {
	if cancel, running := mcpTestCancel[name]; running {
		cancel()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	mcpTestCancel[name] = cancel
	go func() {
		defer cancel()
		info, err := hub.MCPTest(ctx, name)
		out := mcpTestView{Name: name, OK: err == nil, Server: info.Name, Version: info.Version, Tools: info.Tools}
		if errors.Is(ctx.Err(), context.Canceled) {
			out.Canceled = true
		} else if err != nil {
			out.Error = err.Error()
			logf("mcp test %s failed", name)
		}
		runLater(func() {
			delete(mcpTestCancel, name)
			post("u" + fs + "mcp-servers" + fs + "result" + fs + jsonString(out))
			pushMCP("", false)
		})
	}()
}
