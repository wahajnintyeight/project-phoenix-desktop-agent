package providerhub

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"phoenix/internal/provider"
)

// codexToolProxy exposes this run's connected tools to isolated provider CLI processes.
// The random bearer token stays in the child environment or a temporary private config.
type codexToolProxy struct {
	url, token string
	server     *http.Server
}

func startCodexToolProxy(req provider.Request) (*codexToolProxy, error) {
	if req.ExecuteTool == nil {
		return nil, errors.New("tool executor is unavailable")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(secret)
	server := sdk.NewServer(&sdk.Implementation{Name: "phoenix-tools", Version: "1"}, nil)
	var denied atomic.Bool
	for _, tool := range req.Tools {
		var schema map[string]any
		if json.Unmarshal(tool.Parameters, &schema) != nil || schema["type"] != "object" {
			return nil, fmt.Errorf("invalid input schema for tool %s", tool.Name)
		}
		tool := tool
		server.AddTool(&sdk.Tool{Name: tool.Name, Description: tool.Description, InputSchema: schema}, func(ctx context.Context, call *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			fail := func(err error) (*sdk.CallToolResult, error) {
				return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}}}, nil
			}
			if denied.Load() {
				return fail(errors.New("The user declined a tool call; stop and do not retry."))
			}
			args := json.RawMessage(call.Params.Arguments)
			tc := provider.ToolCall{Tool: tool, Arguments: args}
			if tool.RequiresApproval {
				if req.ApproveTool == nil {
					return fail(errors.New("Phoenix approval service is unavailable"))
				}
				if err := req.ApproveTool(ctx, tc); err != nil {
					if errors.Is(err, provider.ErrToolDenied) {
						denied.Store(true)
					}
					return fail(err)
				}
			}
			out, err := req.ExecuteTool(ctx, tc)
			if err != nil {
				if errors.Is(err, provider.ErrToolDenied) {
					denied.Store(true)
				}
				return fail(err)
			}
			content := []sdk.Content{&sdk.TextContent{Text: out.Text}}
			for _, image := range out.Images {
				content = append(content, &sdk.ImageContent{Data: image.Data, MIMEType: image.MIMEType})
			}
			return &sdk.CallToolResult{Content: content, IsError: out.IsError}, nil
		})
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
	slots := make(chan struct{}, 16)
	guarded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		handler.ServeHTTP(w, r)
	})
	httpServer := &http.Server{Handler: guarded, ReadHeaderTimeout: 10 * time.Second}
	proxy := &codexToolProxy{url: "http://" + listener.Addr().String() + "/mcp", token: token, server: httpServer}
	go func() { _ = httpServer.Serve(listener) }()
	return proxy, nil
}

func (p *codexToolProxy) Close() { _ = p.server.Close() }
