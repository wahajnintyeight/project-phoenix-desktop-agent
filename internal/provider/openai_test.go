package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "sk-test-0123456789abcdef"

func spec(t *testing.T, id ID) OpenAICompat {
	t.Helper()
	s, ok := SpecFor(id)
	if !ok {
		t.Fatalf("no spec for %s", id)
	}
	return OpenAICompat{Spec: s}
}

func TestSpecsAreWellFormed(t *testing.T) {
	seen := map[ID]bool{}
	for _, s := range HTTPSpecs {
		if seen[s.ID] || s.Name == "" || !ValidBaseURL(s.BaseURL) || !strings.HasPrefix(s.BaseURL, "https://") || strings.HasSuffix(s.BaseURL, "/") {
			t.Errorf("bad spec %+v", s)
		}
		seen[s.ID] = true
		if !Known(s.ID) || !IsHTTP(s.ID) {
			t.Errorf("%s not registered", s.ID)
		}
	}
	for _, id := range []ID{Gemini, OpenRouter, Mistral, Zai, Grok} {
		if !seen[id] {
			t.Errorf("missing %s", id)
		}
	}
	ids := AllIDs()
	if ids[0] != Claude || ids[1] != Codex || len(ids) != 2+len(HTTPSpecs) {
		t.Errorf("order: %v", ids)
	}
	if Known("evil") || IsHTTP(Claude) {
		t.Error("wrong classification")
	}
}

func TestValidBaseURL(t *testing.T) {
	for _, ok := range []string{"", "https://api.mistral.ai/v1", "https://x.y/a/b", "http://localhost:11434/v1", "http://127.0.0.1:8080", "http://[::1]:9/v1"} {
		if !ValidBaseURL(ok) {
			t.Errorf("rejected %q", ok)
		}
	}
	for _, bad := range []string{"http://evil.example/v1", "http://192.168.1.5/v1", "ftp://x.y", "file:///c:/x", "javascript:alert(1)",
		"https://user:pw@x.y/v1", "https://x.y/v1?token=1", "https://x.y/v1#f", "//x.y", "https://", "x.y/v1", "https://x.y/" + strings.Repeat("a", 250)} {
		if ValidBaseURL(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestDetectNeedsOnlyAKey(t *testing.T) {
	p := spec(t, Mistral)
	st := p.Detect(context.Background(), Settings{})
	if st.Ready || st.Problem == "" || st.Kind != "api" || !st.Installed || st.BaseURL != p.Spec.BaseURL {
		t.Errorf("no key: %+v", st)
	}
	st = p.Detect(context.Background(), Settings{APIKey: testKey, BaseURL: "http://evil.example"})
	if !st.Ready || !st.Login.SignedIn || st.BaseURL != p.Spec.BaseURL {
		t.Errorf("with key (invalid base ignored): %+v", st)
	}
}

func TestRequestBodyByEffortStyle(t *testing.T) {
	body := func(id ID, effort string) map[string]any {
		cr := spec(t, id).buildRequest(Settings{Model: "m"}, Request{Prompt: "hi", System: "be brief", Effort: effort})
		b, _ := json.Marshal(cr)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}
	if m := body(Gemini, "low"); m["reasoning_effort"] != "low" || m["reasoning"] != nil {
		t.Errorf("gemini: %v", m)
	}
	if m := body(OpenRouter, "high"); m["reasoning_effort"] != nil || m["reasoning"].(map[string]any)["effort"] != "high" {
		t.Errorf("openrouter: %v", m)
	}
	for _, id := range []ID{Mistral, Zai, Grok} { // these reject unknown fields, so nothing is sent
		if m := body(id, "high"); m["reasoning_effort"] != nil || m["reasoning"] != nil {
			t.Errorf("%s sent an effort: %v", id, m)
		}
	}
	m := body(Grok, "")
	msgs := m["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || m["stream"] != true || m["reasoning_effort"] != nil {
		t.Errorf("messages: %v", m)
	}
}

const sse = `: keep-alive

data: {"model":"grok-x","choices":[{"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"delta":{"reasoning":"hmm"}}]}

data: {"choices":[{"delta":{"content":"o"}}]}

data: {"choices":[{"delta":{"content":"k"}}]}

data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":2,"cost":0.0004,"prompt_tokens_details":{"cached_tokens":5}}}

data: [DONE]

`

func fakeServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, Settings) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, Settings{Mode: AuthAPIKey, APIKey: testKey, BaseURL: srv.URL + "/v1", Model: "grok-x"}
}

func TestRunStreamsAndSendsKeyOnlyAsBearer(t *testing.T) {
	var gotAuth, gotPath, gotQuery string
	var gotBody map[string]any
	_, s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotQuery = r.Header.Get("Authorization"), r.URL.Path, r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	})
	var evs []Event
	res, err := spec(t, Grok).Run(context.Background(), s, Request{Prompt: "say ok"}, func(e Event) { evs = append(evs, e) })
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer "+testKey || gotPath != "/v1/chat/completions" || gotQuery != "" {
		t.Errorf("auth=%q path=%q query=%q", gotAuth, gotPath, gotQuery)
	}
	if gotBody["model"] != "grok-x" {
		t.Errorf("body %v", gotBody)
	}
	if res.Text != "ok" || res.Usage.InputTokens != 11 || res.Usage.OutputTokens != 2 || res.Usage.CachedTokens != 5 || res.Usage.CostUSD != 0.0004 {
		t.Errorf("result %+v", res)
	}
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, string(e.Kind))
	}
	if got := strings.Join(kinds, ","); got != "start,thinking,text_delta,text_delta,usage,done" {
		t.Errorf("events %s", got)
	}
	if evs[0].Model != "grok-x" {
		t.Errorf("start model %q", evs[0].Model)
	}
}

func TestRunStreamWithoutDoneStillReturnsText(t *testing.T) {
	_, s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	})
	res, err := spec(t, Mistral).Run(context.Background(), s, Request{Prompt: "x"}, func(Event) {})
	if err != nil || res.Text != "partial" {
		t.Errorf("%+v %v", res, err)
	}
}

func TestRunErrorsAreReadableAndNeverEchoTheKey(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"error":{"message":"Incorrect API key provided: ` + testKey + `"}}`, "Check the API key"},
		{429, `{"message":"slow down"}`, "rate limiting"},
		{500, "<html>boom</html>", "HTTP 500"},
	} {
		_, s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		})
		var emitted string
		_, err := spec(t, Zai).Run(context.Background(), s, Request{Prompt: "x"}, func(e Event) { emitted += e.Text })
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Z.ai") {
			t.Errorf("status %d: %v", tc.status, err)
		}
		if err != nil && strings.Contains(err.Error()+emitted, testKey) {
			t.Errorf("status %d leaked the key: %v", tc.status, err)
		}
	}
}

func TestRunMidStreamErrorAndGuards(t *testing.T) {
	_, s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"upstream failed\"}}\n\n")
	})
	res, err := spec(t, OpenRouter).Run(context.Background(), s, Request{Prompt: "x"}, func(Event) {})
	if err != nil || !res.IsError || res.Text != "upstream failed" {
		t.Errorf("%+v %v", res, err)
	}
	p := spec(t, Grok)
	if _, err := p.Run(context.Background(), Settings{}, Request{Prompt: "x", Model: "m"}, func(Event) {}); err != ErrNotReady {
		t.Errorf("no key: %v", err)
	}
	if _, err := p.Run(context.Background(), Settings{APIKey: testKey}, Request{Prompt: "x"}, func(Event) {}); err == nil || !strings.Contains(err.Error(), "choose a model") {
		t.Errorf("no model: %v", err)
	}
	if _, err := p.Run(context.Background(), Settings{APIKey: testKey}, Request{Prompt: "x", Model: "--evil"}, func(Event) {}); err != ErrBadModel {
		t.Errorf("flag-like model: %v", err)
	}
}

func TestRunDoesNotFollowRedirects(t *testing.T) {
	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer target.Close()
	_, s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusTemporaryRedirect)
	})
	_, err := spec(t, Grok).Run(context.Background(), s, Request{Prompt: "x"}, func(Event) {})
	if err == nil || hits != 0 {
		t.Errorf("redirect followed: hits=%d err=%v", hits, err)
	}
}

func TestParseModelsAcrossProviders(t *testing.T) {
	gem, err := spec(t, Gemini).parseModels([]byte(`{"data":[{"id":"models/gemini-b"},{"id":"models/gemini-a"},{"id":"models/text-embedding-9"}]}`))
	if err != nil || len(gem) != 2 || gem[0].ID != "gemini-a" || len(gem[0].Efforts) != 3 {
		t.Errorf("gemini: %+v %v", gem, err)
	}
	mis, _ := spec(t, Mistral).parseModels([]byte(`{"data":[{"id":"mistral-large","description":"Big","max_context_length":128000,"capabilities":{"completion_chat":true}},{"id":"mistral-embed","capabilities":{"completion_chat":false}},{"id":"ocr-x","capabilities":{"completion_chat":false}}]}`))
	if len(mis) != 1 || mis[0].ContextWindow != 128000 || len(mis[0].Efforts) != 0 {
		t.Errorf("mistral: %+v", mis)
	}
	or, _ := spec(t, OpenRouter).parseModels([]byte(`{"data":[{"id":"z/second","name":"Z Second","context_length":200000,"description":"line one\n  line two ` + strings.Repeat("x", 300) + `","architecture":{"output_modalities":["text"]}},{"id":"a/image","architecture":{"output_modalities":["image"]}},{"id":"a/first","name":"A First"}]}`))
	if len(or) != 2 || or[0].ID != "z/second" || or[0].Name != "Z Second" || or[0].ContextWindow != 200000 || strings.Contains(or[0].Description, "\n") || len([]rune(or[0].Description)) > 160 {
		t.Errorf("openrouter keeps its own order and drops image models: %+v", or)
	}
	if _, err := spec(t, Grok).parseModels([]byte(`{"data":[]}`)); err == nil {
		t.Error("an empty list must be an error")
	}
	if _, err := spec(t, Grok).parseModels([]byte(`nope`)); err == nil {
		t.Error("garbage accepted")
	}
	bad, _ := spec(t, Grok).parseModels([]byte(`{"data":[{"id":"--evil"},{"id":"grok-1"},{"id":"grok-1"}]}`))
	if len(bad) != 1 || bad[0].ID != "grok-1" {
		t.Errorf("invalid and duplicate ids must be dropped: %+v", bad)
	}
}

func TestModelsCallsTheModelsEndpoint(t *testing.T) {
	var path, auth string
	_, s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"data":[{"id":"grok-1"}]}`)
	})
	ms, err := spec(t, Grok).Models(context.Background(), s)
	if err != nil || len(ms) != 1 || path != "/v1/models" || auth != "Bearer "+testKey {
		t.Errorf("%+v %v path=%q auth=%q", ms, err, path, auth)
	}
	if _, err := spec(t, Grok).Models(context.Background(), Settings{}); err != ErrNotReady {
		t.Errorf("no key: %v", err)
	}
}

func TestScrub(t *testing.T) {
	if got := scrub("bad "+testKey+" here", testKey); strings.Contains(got, testKey) {
		t.Error(got)
	}
	if scrub("abc", "") != "abc" || scrub("short", "short") != "short" {
		t.Error("tiny keys must not be scrubbed blindly")
	}
}
