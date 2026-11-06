package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// A model without function calling answers the first request with a 400 about tools. Phoenix
// must retry without tools and tell the user, not fail the message.
func TestModelWithoutToolsFallsBackToPlainChat(t *testing.T) {
	var bodies []string
	client := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if strings.Contains(string(b), `"tools"`) {
			return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"this model does not support tools"}}`))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(`data: {"choices":[{"delta":{"content":"hello"}}]}` + "\n\ndata: [DONE]\n\n"))}, nil
	})}
	var spec HTTPSpec
	for _, s := range HTTPSpecs {
		if s.ID == Gemini {
			spec = s
		}
	}
	var errs int
	res, err := OpenAICompat{Spec: spec, Client: client}.Run(context.Background(), Settings{APIKey: "k", Model: "m"}, Request{
		Prompt:      "hi",
		Tools:       []Tool{{Name: "snap", Server: "s", MCPName: "snap", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}},
		ExecuteTool: func(context.Context, ToolCall) (ToolOutput, error) { return ToolOutput{}, nil },
	}, func(e Event) {
		if e.Kind == EvError {
			errs++
		}
	})
	if err != nil || res.IsError || len(bodies) != 2 || errs != 0 {
		t.Fatalf("err=%v isError=%v requests=%d errorEvents=%d", err, res.IsError, len(bodies), errs)
	}
	if !strings.Contains(res.Text, "does not support tools") || !strings.Contains(res.Text, "hello") {
		t.Fatalf("text %q", res.Text)
	}
}

func TestToolsRejectedOnlyForToolErrors(t *testing.T) {
	if toolsRejected(errString("Gemini: bad key (HTTP 401). Check the API key.")) {
		t.Fatal("a bad key is not a tools problem")
	}
	if !toolsRejected(errString("X: tools are not supported (HTTP 400).")) {
		t.Fatal("expected a tools rejection")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
