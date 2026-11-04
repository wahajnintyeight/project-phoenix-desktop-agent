package providerhub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"phoenix/internal/provider"
)

type proxyAuth struct{ token string }

func (a proxyAuth) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestCodexProxyApprovesCallsAndReturnsImages(t *testing.T) {
	approved, called := 0, 0
	req := provider.Request{
		Tools: []provider.Tool{{Name: "mcp_tool_001", Server: "browser", MCPName: "screenshot", Description: "capture", Parameters: json.RawMessage(`{"type":"object","properties":{"tab_id":{"type":"integer"}}}`), RequiresApproval: true}},
		ApproveTool: func(_ context.Context, c provider.ToolCall) error {
			approved++
			if c.Tool.MCPName != "screenshot" {
				t.Error("wrong approval target")
			}
			return nil
		},
		ExecuteTool: func(_ context.Context, c provider.ToolCall) (provider.ToolOutput, error) {
			called++
			return provider.ToolOutput{Text: string(c.Arguments), Images: []provider.ToolImage{{MIMEType: "image/png", Data: []byte{1, 2, 3}}}}, nil
		},
	}
	proxy, err := startCodexToolProxy(req)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	resp, err := http.Get(proxy.url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &sdk.StreamableClientTransport{Endpoint: proxy.url, HTTPClient: &http.Client{Transport: proxyAuth{proxy.token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 {
		t.Fatalf("tools = %v, %v", tools, err)
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "mcp_tool_001", Arguments: map[string]any{"tab_id": 2}})
	if err != nil || result.IsError || len(result.Content) != 2 || approved != 1 || called != 1 {
		t.Fatalf("result=%+v err=%v approved=%d called=%d", result, err, approved, called)
	}
	if image, ok := result.Content[1].(*sdk.ImageContent); !ok || image.MIMEType != "image/png" {
		t.Fatalf("image = %#v", result.Content[1])
	}
}

func TestCodexProxyDeclineStopsFurtherCalls(t *testing.T) {
	approved, called := 0, 0
	req := provider.Request{Tools: []provider.Tool{{Name: "mcp_tool_001", Parameters: json.RawMessage(`{"type":"object"}`), RequiresApproval: true}},
		ApproveTool: func(context.Context, provider.ToolCall) error { approved++; return provider.ErrToolDenied },
		ExecuteTool: func(context.Context, provider.ToolCall) (provider.ToolOutput, error) {
			called++
			return provider.ToolOutput{}, nil
		},
	}
	proxy, err := startCodexToolProxy(req)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &sdk.StreamableClientTransport{Endpoint: proxy.url, HTTPClient: &http.Client{Transport: proxyAuth{proxy.token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for i := 0; i < 2; i++ {
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "mcp_tool_001", Arguments: map[string]any{}})
		if err != nil || !result.IsError {
			t.Fatalf("result=%v err=%v", result, err)
		}
	}
	if approved != 1 || called != 0 {
		t.Fatalf("approved %d, executed %d", approved, called)
	}
}

func TestCodexProxyRejectsInvalidSchema(t *testing.T) {
	_, err := startCodexToolProxy(provider.Request{Tools: []provider.Tool{{Name: "invalid", Parameters: json.RawMessage(`{"type":"string"}`)}}, ExecuteTool: func(context.Context, provider.ToolCall) (provider.ToolOutput, error) {
		return provider.ToolOutput{}, errors.New("unexpected")
	}})
	if err == nil {
		t.Fatal("expected invalid schema error")
	}
}
