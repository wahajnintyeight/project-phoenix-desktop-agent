package main

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"phoenix/internal/desktop"
)

// runDesktopMCP is `phoenix.exe mcp-desktop`: the desktop tools as an MCP server on stdin and
// stdout. The agent's CLI starts it for a chat turn (see desktopServer); it is not meant to
// be run by hand. It asks Phoenix to approve every tool call over loopback HTTP with the
// turn's secret token, and refuses everything when it has no way to ask.
func runDesktopMCP() int {
	path := os.Getenv("PHOENIX_DESKTOP_CLI")
	if path == "" {
		p, ok := desktop.Find("")
		if !ok {
			fmt.Fprintln(os.Stderr, "agent-desktop is not installed")
			return 1
		}
		path = p
	}
	var approver desktop.Approver = desktop.DenyAll{}
	if url, token := os.Getenv("PHOENIX_APPROVAL_URL"), os.Getenv("PHOENIX_APPROVAL_TOKEN"); url != "" && token != "" {
		approver = desktop.HTTPApprover{URL: url, Token: token}
	}
	srv := desktop.NewServer(desktop.CLI{Path: path}, approver)
	if err := srv.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "desktop server:", err)
		return 1
	}
	return 0
}
