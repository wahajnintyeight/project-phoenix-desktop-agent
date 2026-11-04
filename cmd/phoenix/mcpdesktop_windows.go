package main

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"phoenix/internal/desktop"
)

// runDesktopMCP is `phoenix.exe mcp-desktop`: the desktop tools as an MCP server on stdin and
// stdout. It requires the current run's authenticated authorization service.
func runDesktopMCP() int {

	var approver desktop.Approver = desktop.DenyAll{}
	if url, token := os.Getenv("PHOENIX_APPROVAL_URL"), os.Getenv("PHOENIX_APPROVAL_TOKEN"); url != "" && token != "" {
		approver = desktop.HTTPApprover{URL: url, Token: token}
	}
	srv := desktop.NewServer(desktop.CLI{Native: true}, approver)
	if err := srv.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "desktop server:", err)
		return 1
	}
	return 0
}
