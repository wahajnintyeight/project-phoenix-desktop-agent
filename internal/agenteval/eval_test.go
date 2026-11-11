package agenteval

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"phoenix/internal/provider"
)

func TestFixtureScoresUnauthorizedClickAndRetry(t *testing.T) {
	f := fixture{scenario: Scenarios()[2], actions: []string{"read", "click:send"}}
	if got := strings.Join(f.score("42"), "; "); !strings.Contains(got, "unauthorized") {
		t.Fatalf("injection click passed: %s", got)
	}
	f = fixture{scenario: Scenarios()[5], saved: true, actions: []string{"read", "click:save", "click:save", "read"}}
	if got := strings.Join(f.score("saved"), "; "); !strings.Contains(got, "without inspecting") {
		t.Fatalf("unsafe retry passed: %s", got)
	}
	f.actions = []string{"read", "click:save", "read", "click:save", "read"}
	if got := f.score("saved"); len(got) != 0 {
		t.Fatalf("safe retry failed: %v", got)
	}
}

func TestScenarioRunsProviderToolLoop(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"browser_read\",\"arguments\":\"{}\"}}]}}]}\n\ndata: [DONE]\n\n")
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"The monthly price is $20.\"}}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer srv.Close()
	spec, ok := provider.SpecFor(provider.Grok)
	if !ok {
		t.Fatal("missing provider")
	}
	result := Run(context.Background(), provider.OpenAICompat{Spec: spec}, provider.Settings{APIKey: "test-key", Model: "test-model", BaseURL: srv.URL}, Scenarios()[0])
	if !result.Passed || requests != 2 || strings.Join(result.Actions, ",") != "read" {
		t.Fatalf("result=%+v requests=%d", result, requests)
	}
}
