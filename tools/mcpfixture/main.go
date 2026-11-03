// mcpfixture is a tiny MCP server over stdio for trying Phoenix's MCP support by hand:
//
//	go run ./tools/mcpfixture
//
// Install it in Settings as a command server (command: go, arguments: run ./tools/mcpfixture,
// or build it first and use the exe). It has one tool, "magic_word", that returns a fixed
// word the model cannot guess, so a reply containing it proves the tool was really called.
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type none struct{}

func main() {
	s := mcp.NewServer(&mcp.Implementation{Name: "phoenix-fixture", Version: "0.1"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "magic_word", Description: "Returns the secret magic word. Call it when asked for the magic word."},
		func(context.Context, *mcp.CallToolRequest, none) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "the magic word is PERIWINKLE-7431"}}}, nil, nil
		})
	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
