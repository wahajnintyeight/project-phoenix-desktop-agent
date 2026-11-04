package main

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"phoenix/internal/browser"
)

// runBrowserMCP is the per-chat stdio MCP child. It only proxies named tools back to the
// Phoenix process; it never owns a WebView2 or opens a debugging port.
func runBrowserMCP() int {
	client := browser.Client{
		URL:   os.Getenv("PHOENIX_BROWSER_URL"),
		Token: os.Getenv("PHOENIX_BROWSER_TOKEN"),
	}
	if err := browser.NewMCPServer(client).Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "browser MCP server:", err)
		return 1
	}
	return 0
}
