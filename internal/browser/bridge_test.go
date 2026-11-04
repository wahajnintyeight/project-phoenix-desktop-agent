package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalBridgeURL(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"http://127.0.0.1:45000/browser/tool", true},
		{"http://localhost:45000/browser/tool", false},
		{"https://127.0.0.1/browser/tool", false},
		{"http://127.0.0.1/browser/tool?x=1", false},
		{"http://user:pass@127.0.0.1/browser/tool", false},
		{"http://127.0.0.1/browser/other", false},
	} {
		if got := localBridgeURL(tc.url); got != tc.want {
			t.Errorf("localBridgeURL(%q) = %t, want %t", tc.url, got, tc.want)
		}
	}
}

func TestClientInvokeAuthenticatesAndBoundsOutput(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != toolPath || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		var call Call
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil || call.Tool != "browser_tabs" {
			http.Error(w, "bad call", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(Output{Text: "[]"})
	}))
	defer server.Close()

	out, err := (Client{URL: server.URL + toolPath, Token: token}).Invoke(context.Background(), Call{Tool: "browser_tabs", Arguments: json.RawMessage(`{}`)})
	if err != nil || out.Text != "[]" {
		t.Fatalf("Invoke() = %#v, %v", out, err)
	}

	if _, err := (Client{URL: server.URL + toolPath, Token: "short"}).Invoke(context.Background(), Call{Tool: "browser_tabs"}); err == nil {
		t.Fatal("short token was accepted")
	}

	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Output{Text: strings.Repeat("x", maxText+1)})
	}))
	defer oversized.Close()
	if _, err := (Client{URL: oversized.URL + toolPath, Token: token}).Invoke(context.Background(), Call{Tool: "browser_tabs"}); err == nil {
		t.Fatal("oversized tool output was accepted")
	}
}
