package providerhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"phoenix/internal/mcp"
	"phoenix/internal/provider"
)

type toolProbe struct{ fake }

func (p *toolProbe) Run(ctx context.Context, _ provider.Settings, req provider.Request, _ func(provider.Event)) (provider.Result, error) {
	endpoint, token := req.CodexMCPURL, req.CodexMCPToken
	if p.id == provider.Claude {
		raw, err := os.ReadFile(req.MCPConfig)
		if err != nil {
			return provider.Result{}, err
		}
		var config struct {
			Servers map[string]struct {
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			return provider.Result{}, err
		}
		entry := config.Servers["phoenix-tools"]
		endpoint, token = entry.URL, strings.TrimPrefix(entry.Headers["Authorization"], "Bearer ")
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "provider-probe", Version: "1"}, nil).Connect(ctx, &sdk.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: proxyAuth{token}}}, nil)
	if err != nil {
		return provider.Result{}, err
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 1 {
		return provider.Result{}, fmt.Errorf("list: %v", err)
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: list.Tools[0].Name, Arguments: map[string]any{"value": "hello"}})
	if err != nil || result.IsError {
		return provider.Result{}, fmt.Errorf("call: %v, %v", result, err)
	}
	return provider.Result{Text: result.Content[0].(*sdk.TextContent).Text}, nil
}

func TestCLIProvidersUsePhoenixMCPRuntime(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	type input struct {
		Value string `json:"value"`
	}
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "echo the value"}, func(_ context.Context, _ *sdk.CallToolRequest, in input) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: in.Value}}}, nil, nil
	})
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
	upstream := httptest.NewServer(handler)
	defer upstream.Close()
	for _, id := range []provider.ID{provider.Claude, provider.Codex} {
		t.Run(string(id), func(t *testing.T) {
			h, _, _, _ := newHub(t)
			h.providers[id] = &toolProbe{fake: fake{id: id}}
			approved := 0
			request := provider.Request{MCPServers: []mcp.Resolved{{Server: mcp.Server{Name: "fixture", Transport: mcp.HTTP, URL: upstream.URL, Enabled: true}, RequireApproval: true}}, ApproveTool: func(context.Context, provider.ToolCall) error { approved++; return nil }}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := h.Run(ctx, id, request, func(provider.Event) {})
			if err != nil || result.Text != "hello" || approved != 1 {
				t.Fatalf("result=%+v approval=%d err=%v", result, approved, err)
			}
		})
	}
}
func TestLiveCodexUsesPhoenixTool(t *testing.T) {
	if os.Getenv("PHOENIX_LIVE_CODEX_TEST") != "1" {
		t.Skip("set PHOENIX_LIVE_CODEX_TEST=1 to run against the signed-in CLI")
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	type input struct {
		Value string `json:"value"`
	}
	calls := 0
	approved := 0
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "Returns the supplied value"}, func(_ context.Context, _ *sdk.CallToolRequest, in input) (*sdk.CallToolResult, any, error) {
		calls++
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: in.Value}}}, nil, nil
	})
	upstream := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil))
	defer upstream.Close()
	h, _, _, _ := newHub(t)
	h.providers[provider.Codex] = provider.CodexProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := h.Run(ctx, provider.Codex, provider.Request{Prompt: "Use the available echo tool with value phoenix-tool-check, then return its result. You must call the tool.", MCPServers: []mcp.Resolved{{Server: mcp.Server{Name: "fixture", Transport: mcp.HTTP, URL: upstream.URL, Enabled: true}, RequireApproval: true}}, ApproveTool: func(context.Context, provider.ToolCall) error { approved++; return nil }}, func(provider.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || calls != 1 || approved != 1 || !strings.Contains(result.Text, "phoenix-tool-check") {
		t.Fatalf("result=%+v calls=%d approved=%d", result, calls, approved)
	}
}
func TestUnavailableServerDoesNotBlockOtherTools(t *testing.T) {
	goodServer := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	type input struct {
		Value string `json:"value"`
	}
	sdk.AddTool(goodServer, &sdk.Tool{Name: "echo"}, func(_ context.Context, _ *sdk.CallToolRequest, _ input) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ok"}}}, nil, nil
	})
	good := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return goodServer }, nil))
	defer good.Close()
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	h, _, _, _ := newHub(t)
	probe := &captureRun{fake: fake{id: provider.Codex}}
	h.providers[provider.Codex] = probe
	req := provider.Request{MCPServers: []mcp.Resolved{{Server: mcp.Server{Name: "broken", Transport: mcp.HTTP, URL: bad.URL, Enabled: true}}, {Server: mcp.Server{Name: "working", Transport: mcp.HTTP, URL: good.URL, Enabled: true}}}}
	var events []provider.Event
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := h.Run(ctx, provider.Codex, req, func(e provider.Event) { events = append(events, e) })
	if err != nil || len(probe.req.Tools) != 1 || !strings.Contains(probe.req.System, "broken") || len(events) != 1 || !strings.Contains(events[0].ToolName, "broken") {
		t.Fatalf("tools=%v events=%v err=%v", probe.req.Tools, events, err)
	}
}
