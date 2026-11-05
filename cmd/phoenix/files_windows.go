package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"phoenix/internal/mcp"
	"phoenix/internal/workspace"
)

// workspaceDir is where the agent's files live: the configured folder, else Documents\Phoenix.
func workspaceDir() string {
	if hub != nil {
		if d := hub.WorkspaceDir(); d != "" {
			return d
		}
	}
	if d := os.Getenv("PHOENIX_WORKSPACE"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Documents", "Phoenix")
}

// openWorkspace opens (creating if needed) the current workspace.
func openWorkspace() (*workspace.Root, error) {
	dir := workspaceDir()
	if dir == "" {
		return nil, fmt.Errorf("no workspace folder")
	}
	return workspace.Open(dir)
}

// filesServer is the per-run MCP child that serves the file tools for one workspace. The
// folder is passed by environment; the child enforces confinement itself.
func filesServer(root *workspace.Root) (mcp.Resolved, error) {
	exe, err := os.Executable()
	if err != nil {
		return mcp.Resolved{}, err
	}
	return mcp.Resolved{
		Server:    mcp.Server{Name: workspace.ServerName, Transport: mcp.Stdio, Command: exe, Args: []string{"mcp-files"}, Enabled: true},
		EnvValues: map[string]string{"PHOENIX_WORKSPACE": root.Dir()},
	}, nil
}

// runFilesMCP is `phoenix.exe mcp-files`.
func runFilesMCP() int {
	root, err := workspace.Open(os.Getenv("PHOENIX_WORKSPACE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "files server:", err)
		return 1
	}
	if err := workspace.NewServer(root).Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "files server:", err)
		return 1
	}
	return 0
}
