package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A tool with no parameters comes back with empty arguments; replaying "" is invalid JSON and
// Gemini answers the follow-up request with 400 INVALID_ARGUMENT.
func TestEmptyToolArgumentsAreReplayedAsAnObject(t *testing.T) {
	var bodies []string
	sse := func(s string) *http.Response {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(s))}
	}
	client := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			return sse(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"snap","arguments":""}}]}}]}` + "\n\n" + "data: [DONE]\n\n"), nil
		}
		return sse(`data: {"choices":[{"delta":{"content":"ok"}}]}` + "\n\n" + "data: [DONE]\n\n"), nil
	})}
	var spec HTTPSpec
	for _, s := range HTTPSpecs {
		if s.ID == Gemini {
			spec = s
		}
	}
	p := OpenAICompat{Spec: spec, Client: client}
	_, err := p.Run(context.Background(), Settings{APIKey: "k", Model: "m"}, Request{
		Prompt: "go",
		Tools:  []Tool{{Name: "snap", Server: "s", MCPName: "snap", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}},
		ExecuteTool: func(context.Context, ToolCall) (ToolOutput, error) {
			return ToolOutput{Text: "done"}, nil
		},
	}, func(Event) {})
	if err != nil || len(bodies) != 2 {
		t.Fatalf("err=%v requests=%d", err, len(bodies))
	}
	if !strings.Contains(bodies[1], `"arguments":"{}"`) {
		t.Fatalf("follow-up request replays empty arguments: %s", bodies[1])
	}
}
