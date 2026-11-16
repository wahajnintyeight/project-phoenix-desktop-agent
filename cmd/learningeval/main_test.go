package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEvaluationRequiresOptInAndBoundsRequests(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			MaxTokens int                              `json:"max_tokens"`
			Messages  []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.MaxTokens != 96 || len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
			t.Errorf("unexpected bounded request: %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ask sponsor latest\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	t.Setenv("PHOENIX_EVAL_PROVIDER", "grok")
	t.Setenv("PHOENIX_EVAL_MODEL", "fixture")
	t.Setenv("PHOENIX_EVAL_API_KEY", "fixture-key")
	t.Setenv("PHOENIX_EVAL_BASE_URL", server.URL)
	t.Setenv("PHOENIX_LEARNING_EVAL_ACK", "")
	if err := run(); err == nil || calls.Load() != 0 {
		t.Fatal("evaluation ran without explicit opt-in")
	}
	t.Setenv("PHOENIX_LEARNING_EVAL_ACK", "synthetic-9-calls")
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 9 {
		t.Fatalf("got %d calls, want 9", got)
	}
}
