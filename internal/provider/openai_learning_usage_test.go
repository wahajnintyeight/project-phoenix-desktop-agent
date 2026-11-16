package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestToolRoundUsageAndPayload(t *testing.T) {
	for _, missingSecond := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "missing-second"}[missingSecond], func(t *testing.T) {
			var bodies []string
			response := func(body string) *http.Response {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
			}
			client := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
				b, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				bodies = append(bodies, string(b))
				if len(bodies) == 1 {
					return response("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"learning_search\",\"arguments\":\"{}\"}}]}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"cost\":0.001,\"prompt_tokens_details\":{\"cached_tokens\":4}}}\n\ndata: [DONE]\n\n"), nil
				}
				usage := ""
				if !missingSecond {
					usage = "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":3,\"cost\":0.002,\"prompt_tokens_details\":{\"cached_tokens\":5}}}\n\n"
				}
				return response("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" + usage + "data: [DONE]\n\n"), nil
			})}
			p := spec(t, Grok)
			p.Client = client
			calls := 0
			result, err := p.Run(context.Background(), Settings{APIKey: "test", Model: "test"}, Request{Prompt: "question", Tools: []Tool{{Name: "learning_search", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}, ExecuteTool: func(context.Context, ToolCall) (ToolOutput, error) {
				calls++
				return ToolOutput{Text: "approved synthetic lesson"}, nil
			}}, func(Event) {})
			if err != nil || len(bodies) != 2 || calls != 1 {
				t.Fatalf("rounds=%d calls=%d err=%v", len(bodies), calls, err)
			}
			if !strings.Contains(bodies[0], "learning_search") || !strings.Contains(bodies[1], "approved synthetic lesson") {
				t.Fatal("tool schema or result missing from request")
			}
			u := result.Usage
			if missingSecond {
				if u.InputReported || u.OutputReported || u.CachedReported || u.CostReported {
					t.Fatalf("partial usage marked complete: %+v", u)
				}
				return
			}
			if !u.InputReported || !u.OutputReported || !u.CachedReported || !u.CostReported || u.InputTokens != 30 || u.OutputTokens != 5 || u.CachedTokens != 9 || u.CostUSD < 0.00299 || u.CostUSD > 0.00301 {
				t.Fatalf("incorrect aggregate: %+v", u)
			}
		})
	}
}
