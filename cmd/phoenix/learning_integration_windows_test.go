package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"phoenix/internal/library"
	"phoenix/internal/provider"
)

// Exercise the real local MCP, review, prompt assembly and HTTP tool round trip.
func TestLearningFromProposalToLaterRequest(t *testing.T) {
	ctx := context.Background()
	s, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	a, b := sdk.NewInMemoryTransports()
	server, err := library.NewServerForSession(s, "agent", "fixture-chat").Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	proposed, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "learning_propose", Arguments: map[string]any{"kind": "workflow", "trigger": "visa sponsorship", "lesson": "Check sponsor policy", "evidence": "User correction"}})
	if err != nil || proposed.IsError {
		t.Fatalf("proposal: %+v %v", proposed, err)
	}
	if got := memoryContext(s, "visa sponsorship", nil, nil, ""); got != "" {
		t.Fatalf("candidate leaked: %q", got)
	}
	lessons, err := s.Lessons()
	if err != nil || len(lessons) != 1 {
		t.Fatalf("lessons: %+v %v", lessons, err)
	}
	if err := s.ReviewLesson(lessons[0].ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLearningEnabled(false); err != nil {
		t.Fatal(err)
	}
	memory := memoryContext(s, "visa sponsorship", nil, nil, "")
	if !strings.Contains(memory, "Check sponsor policy") {
		t.Fatalf("approved lesson missing: %q", memory)
	}
	listed, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var schema []byte
	for _, tool := range listed.Tools {
		if tool.Name == "learning_search" {
			schema, err = json.Marshal(tool.InputSchema)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(schema) == 0 {
		t.Fatal("search tool missing")
	}
	var requests []string
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		requests = append(requests, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		if len(requests) == 1 {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"learning_search\",\"arguments\":\"{\\\"query\\\":\\\"visa sponsorship\\\"}\"}}]}}]}\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer httpServer.Close()
	spec, ok := provider.SpecFor(provider.Grok)
	if !ok {
		t.Fatal("provider missing")
	}
	p := provider.OpenAICompat{Spec: spec}
	calls := 0
	result, err := p.Run(ctx, provider.Settings{APIKey: "synthetic", Model: "test", BaseURL: httpServer.URL}, provider.Request{
		Prompt: promptWithMemory("visa sponsorship", memory), Tools: []provider.Tool{{Name: "learning_search", Parameters: schema}},
		ExecuteTool: func(ctx context.Context, call provider.ToolCall) (provider.ToolOutput, error) {
			calls++
			var args map[string]any
			if err := json.Unmarshal(call.Arguments, &args); err != nil {
				return provider.ToolOutput{}, err
			}
			reply, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "learning_search", Arguments: args})
			if err != nil {
				return provider.ToolOutput{}, err
			}
			return provider.ToolOutput{IsError: reply.IsError, Text: reply.Content[0].(*sdk.TextContent).Text}, nil
		},
	}, func(provider.Event) {})
	if err != nil || result.IsError || len(requests) != 2 || calls != 1 {
		t.Fatalf("result=%+v requests=%d calls=%d err=%v", result, len(requests), calls, err)
	}
	if !strings.Contains(requests[0], "Check sponsor policy") {
		t.Fatal("approved memory missing from first request")
	}
	var followup struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(requests[1]), &followup); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range followup.Messages {
		if message.Role == "tool" && strings.Contains(message.Content, "Check sponsor policy") {
			found = true
		}
	}
	if !found {
		t.Fatal("approved search result missing from tool response")
	}
}
