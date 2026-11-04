package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOpenAvailableKeepsWorkingServer(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	type input struct {
		Value string `json:"value"`
	}
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "echo"}, func(_ context.Context, _ *sdk.CallToolRequest, in input) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: in.Value}}}, nil, nil
	})
	good := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil))
	defer good.Close()
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	servers := []Resolved{{Server: Server{Name: "broken", Transport: HTTP, URL: bad.URL, Enabled: true}}, {Server: Server{Name: "working", Transport: HTTP, URL: good.URL, Enabled: true}}}
	rt, missing, err := OpenAvailable(ctx, servers)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if len(missing) != 1 || missing[0] != "broken" || len(rt.Functions()) != 1 {
		t.Fatalf("missing=%v functions=%v", missing, rt.Functions())
	}
	result, err := rt.Call(ctx, rt.Functions()[0].Name, json.RawMessage(`{"value":"ready"}`))
	if err != nil || result.Text != "ready" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
