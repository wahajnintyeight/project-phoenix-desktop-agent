package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Providers that have no command-line client are reached over HTTP. Gemini, OpenRouter,
// Mistral, Z.ai and Grok all publish an OpenAI-compatible chat API (POST /chat/completions
// with server-sent events, GET /models), so one small driver covers them. It uses only the
// standard library, so no third-party code ever handles an API key.
const (
	Gemini     ID = "gemini"
	OpenRouter ID = "openrouter"
	Mistral    ID = "mistral"
	Zai        ID = "zai"
	Grok       ID = "grok"
)

type effortStyle int

const (
	effortNone       effortStyle = iota
	effortTopLevel               // "reasoning_effort": "low"        (Gemini's OpenAI-compatible API)
	effortReasonings             // "reasoning": {"effort": "low"}    (OpenRouter)
)

// HTTPSpec describes one OpenAI-compatible provider.
type HTTPSpec struct {
	ID       ID
	Name     string
	BaseURL  string // default; the user may point it elsewhere (for example a regional endpoint)
	KeyHelp  string // where to get a key, shown as text only
	BaseHint string // when another base URL is needed
	effort   effortStyle
}

// HTTPSpecs lists the HTTP providers in display order. Base URLs come from each vendor's
// documentation; none of them is probed at start-up.
var HTTPSpecs = []HTTPSpec{
	{ID: Gemini, Name: "Gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		KeyHelp: "Create a key in Google AI Studio (aistudio.google.com).", effort: effortTopLevel},
	{ID: OpenRouter, Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1",
		KeyHelp: "Create a key at openrouter.ai/keys. One key reaches many vendors' models.", effort: effortReasonings},
	{ID: Mistral, Name: "Mistral", BaseURL: "https://api.mistral.ai/v1",
		KeyHelp: "Create a key in the Mistral console (console.mistral.ai)."},
	{ID: Zai, Name: "Z.ai", BaseURL: "https://api.z.ai/api/paas/v4",
		KeyHelp:  "Create a key in your Z.ai account.",
		BaseHint: "GLM Coding Plan keys use https://api.z.ai/api/coding/paas/v4. Keys for the China endpoint use https://open.bigmodel.cn/api/paas/v4."},
	{ID: Grok, Name: "Grok (xAI)", BaseURL: "https://api.x.ai/v1",
		KeyHelp: "Create a key in the xAI console (console.x.ai)."},
}

var httpEfforts = []string{"low", "medium", "high"}

// SpecFor returns the HTTP spec for id.
func SpecFor(id ID) (HTTPSpec, bool) {
	for _, s := range HTTPSpecs {
		if s.ID == id {
			return s, true
		}
	}
	return HTTPSpec{}, false
}

// AllIDs lists every provider in display order: the two CLI providers, then the HTTP ones.
func AllIDs() []ID {
	ids := []ID{Claude, Codex}
	for _, s := range HTTPSpecs {
		ids = append(ids, s.ID)
	}
	return ids
}

// Known reports whether id is a provider Phoenix has.
func Known(id ID) bool {
	if id == Claude || id == Codex {
		return true
	}
	_, ok := SpecFor(id)
	return ok
}

// IsHTTP reports whether id is an HTTP (API key only) provider.
func IsHTTP(id ID) bool { _, ok := SpecFor(id); return ok }

// ValidBaseURL accepts "" (the provider's default) or an https URL. Plain http is allowed
// only for this machine, for local servers. Credentials, queries and fragments are refused:
// the API key is sent to this address, so it must be a plain, deliberate origin.
func ValidBaseURL(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 200 {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		if h == "localhost" {
			return true
		}
		ip := net.ParseIP(h)
		return ip != nil && ip.IsLoopback()
	}
	return false
}

// OpenAICompat drives one OpenAI-compatible HTTP provider.
type OpenAICompat struct {
	Spec   HTTPSpec
	Client *http.Client // nil uses a client that never follows redirects
}

// NewHTTPProviders returns a driver for every spec.
func NewHTTPProviders() []Provider {
	out := make([]Provider, len(HTTPSpecs))
	for i, s := range HTTPSpecs {
		out[i] = OpenAICompat{Spec: s}
	}
	return out
}

func (p OpenAICompat) ID() ID { return p.Spec.ID }

func (p OpenAICompat) base(s Settings) string {
	b := s.BaseURL
	if b == "" || !ValidBaseURL(b) {
		b = p.Spec.BaseURL
	}
	return strings.TrimRight(b, "/")
}

func (p OpenAICompat) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{
		// A redirect could carry the API key to another host, and these APIs do not redirect.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 60 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
		},
	}
}

func (p OpenAICompat) Detect(_ context.Context, s Settings) Status {
	st := Status{
		ID: p.Spec.ID, Name: p.Spec.Name, Kind: "api", Installed: true, Mode: AuthAPIKey,
		HasAPIKey: s.APIKey != "", BaseURL: p.base(s), DefaultBaseURL: p.Spec.BaseURL,
		KeyHelp: p.Spec.KeyHelp, BaseHint: p.Spec.BaseHint,
	}
	if s.APIKey == "" {
		st.Problem = "No API key is saved for " + p.Spec.Name + "."
		return st
	}
	st.Login = Login{SignedIn: true, Method: "api-key"}
	st.Ready = true
	return st
}

func (OpenAICompat) Login(context.Context, Settings) error {
	return errors.New("this provider has no sign-in; use an API key")
}

// Models asks the provider which models the key can use. Nothing is guessed: a model list
// that failed to load is reported, and the user can still type a model id.
func (p OpenAICompat) Models(ctx context.Context, s Settings) ([]Model, error) {
	if s.APIKey == "" {
		return nil, ErrNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base(s)+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, errors.New(scrub(err.Error(), s.APIKey))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, p.httpError(resp.StatusCode, body, s.APIKey)
	}
	return p.parseModels(body)
}

func (p OpenAICompat) parseModels(b []byte) ([]Model, error) {
	var list struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			ContextLength int    `json:"context_length"`     // OpenRouter
			MaxContext    int    `json:"max_context_length"` // Mistral
			ContextWindow int    `json:"context_window"`
			Capabilities  *struct {
				Chat *bool `json:"completion_chat"` // Mistral
			} `json:"capabilities"`
			Architecture *struct {
				Output []string `json:"output_modalities"` // OpenRouter
			} `json:"architecture"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, errors.New("the model list was not in the expected format")
	}
	var out []Model
	seen := map[string]bool{}
	for _, m := range list.Data {
		id := strings.TrimPrefix(m.ID, "models/") // Gemini's compatible API prefixes ids
		low := strings.ToLower(id)
		if id == "" || seen[id] || !ValidModelID(id) || strings.Contains(low, "embed") || strings.Contains(low, "moderation") {
			continue
		}
		if m.Capabilities != nil && m.Capabilities.Chat != nil && !*m.Capabilities.Chat {
			continue
		}
		if m.Architecture != nil && len(m.Architecture.Output) > 0 && !contains(m.Architecture.Output, "text") {
			continue
		}
		seen[id] = true
		ctxLen := firstPositive(m.ContextLength, m.MaxContext, m.ContextWindow)
		name := m.Name
		if name == "" {
			name = id
		}
		out = append(out, Model{ID: id, Name: name, Description: clip(oneLine(m.Description), 160), ContextWindow: ctxLen, Efforts: p.efforts()})
	}
	if len(out) == 0 {
		return nil, errors.New("the provider returned no chat models for this key")
	}
	if p.Spec.ID != OpenRouter { // OpenRouter's own order groups by popularity
		sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	}
	return out, nil
}

func (p OpenAICompat) efforts() []string {
	if p.Spec.effort == effortNone {
		return nil
	}
	return httpEfforts
}

type chatMessage struct {
	Role       string              `json:"role"`
	Content    any                 `json:"content"`
	Name       string              `json:"name,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	ToolCalls  []assistantToolCall `json:"tool_calls,omitempty"`
}

type assistantToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type apiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type apiTool struct {
	Type     string      `json:"type"`
	Function apiFunction `json:"function"`
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	Stream          bool          `json:"stream"`
	Tools           []apiTool     `json:"tools,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	Reasoning       *struct {
		Effort string `json:"effort"`
	} `json:"reasoning,omitempty"`
}

func (p OpenAICompat) buildRequest(s Settings, req Request) chatRequest {
	cr := chatRequest{Model: firstNonEmpty(req.Model, s.Model), Stream: true}
	if req.System != "" {
		cr.Messages = append(cr.Messages, chatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.History {
		if m.Role == "user" || m.Role == "assistant" { // anything else could smuggle a system turn
			cr.Messages = append(cr.Messages, chatMessage{Role: m.Role, Content: m.Content})
		}
	}
	cr.Messages = append(cr.Messages, chatMessage{Role: "user", Content: req.Prompt})
	for _, tool := range req.Tools {
		cr.Tools = append(cr.Tools, apiTool{Type: "function", Function: apiFunction{
			Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters,
		}})
	}
	if e := firstNonEmpty(req.Effort, s.Effort); e != "" {
		switch p.Spec.effort {
		case effortTopLevel:
			cr.ReasoningEffort = e
		case effortReasonings:
			cr.Reasoning = &struct {
				Effort string `json:"effort"`
			}{e}
		}
	}
	return cr
}

// httpError turns an error response into one readable sentence. Providers return
// {"error":{"message":...}} or {"message":...}; anything else is clipped.
func (p OpenAICompat) httpError(status int, body []byte, key string) error {
	var e struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := ""
	if json.Unmarshal(body, &e) == nil {
		msg = firstNonEmpty(e.Error.Message, e.Message, e.Detail)
	}
	if msg == "" {
		msg = clip(oneLine(string(body)), 300)
	}
	hint := ""
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		hint = " Check the API key."
	case http.StatusTooManyRequests:
		hint = " The provider is rate limiting this key."
	}
	return fmt.Errorf("%s: %s (HTTP %d).%s", p.Spec.Name, scrub(clip(msg, 300), key), status, hint)
}

// openaiParser turns "data: {...}" lines from a chat completions stream into Events.
type openaiParser struct {
	started bool
	done    bool
	text    strings.Builder
	usage   *Usage
	result  Result
	calls   map[int]*partialToolCall
}

type partialToolCall struct {
	id        string
	typeName  string
	name      strings.Builder
	arguments strings.Builder
}

func (p *openaiParser) Parse(line []byte) []Event {
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte("data:")) { // blank lines and ": keep-alive" comments
		return nil
	}
	data := bytes.TrimSpace(line[5:])
	if string(data) == "[DONE]" {
		return p.finish()
	}
	var c struct {
		Model   string `json:"model"`
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				Reasoning        string `json:"reasoning"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *struct {
			Prompt  int     `json:"prompt_tokens"`
			Output  int     `json:"completion_tokens"`
			Cost    float64 `json:"cost"` // OpenRouter
			Details *struct {
				Cached int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &c) != nil {
		return nil
	}
	var evs []Event
	if c.Error != nil && c.Error.Message != "" { // an error reported inside the stream
		p.done = true
		p.result.IsError = true
		p.result.Text = c.Error.Message
		return []Event{{Kind: EvError, Text: c.Error.Message, IsError: true}, {Kind: EvDone, Text: c.Error.Message, IsError: true}}
	}
	if !p.started {
		p.started = true
		evs = append(evs, Event{Kind: EvStart, Model: c.Model})
	}
	for _, ch := range c.Choices {
		if t := firstNonEmpty(ch.Delta.Reasoning, ch.Delta.ReasoningContent); t != "" {
			evs = append(evs, Event{Kind: EvThinking, Text: t})
		}
		if ch.Delta.Content != "" {
			p.text.WriteString(ch.Delta.Content)
			evs = append(evs, Event{Kind: EvTextDelta, Text: ch.Delta.Content})
		}
		for _, delta := range ch.Delta.ToolCalls {
			if delta.Index < 0 || delta.Index >= maxToolCalls {
				return p.failToolStream("The provider returned too many or invalid function calls.")
			}
			if p.calls == nil {
				p.calls = map[int]*partialToolCall{}
			}
			call := p.calls[delta.Index]
			if call == nil {
				if len(p.calls) >= maxToolCalls {
					return p.failToolStream("The provider returned too many function calls.")
				}
				call = &partialToolCall{}
				p.calls[delta.Index] = call
			}
			if delta.ID != "" {
				if len(delta.ID) > 128 {
					return p.failToolStream("The provider returned an invalid function-call ID.")
				}
				call.id = delta.ID
			}
			if delta.Type != "" {
				if len(delta.Type) > 32 {
					return p.failToolStream("The provider returned an invalid function-call type.")
				}
				call.typeName = delta.Type
			}
			mergeStream(&call.name, delta.Function.Name)
			mergeStream(&call.arguments, delta.Function.Arguments)
			if call.name.Len() > 64 || call.arguments.Len() > maxToolArgs {
				return p.failToolStream("A provider function call exceeded Phoenix's size limit.")
			}
		}
	}
	if c.Usage != nil {
		u := &Usage{InputTokens: c.Usage.Prompt, OutputTokens: c.Usage.Output, CostUSD: c.Usage.Cost}
		if c.Usage.Details != nil {
			u.CachedTokens = c.Usage.Details.Cached
		}
		p.usage = u
	}
	return evs
}

func (p *openaiParser) failToolStream(message string) []Event {
	p.done = true
	p.result.IsError = true
	p.result.Text = message
	if p.usage != nil {
		p.result.Usage = *p.usage
	}
	return []Event{{Kind: EvError, Text: message, IsError: true}, {Kind: EvDone, Text: message, IsError: true}}
}

func mergeStream(dst *strings.Builder, part string) {
	current := dst.String()
	if part == "" || part == current || strings.HasPrefix(current, part) {
		return
	}
	if strings.HasPrefix(part, current) {
		dst.Reset()
	}
	dst.WriteString(part)
}

func (p *openaiParser) toolCalls() []assistantToolCall {
	indices := make([]int, 0, len(p.calls))
	for i := range p.calls {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	calls := make([]assistantToolCall, 0, len(indices))
	for _, i := range indices {
		c := p.calls[i]
		typeName := c.typeName
		if typeName == "" {
			typeName = "function"
		}
		calls = append(calls, assistantToolCall{
			ID: c.id, Type: typeName,
			Function: functionCall{Name: c.name.String(), Arguments: c.arguments.String()},
		})
	}
	return calls
}

func (p *openaiParser) finish() []Event {
	if p.done {
		return nil
	}
	p.done = true
	p.result.Text = p.text.String()
	var evs []Event
	if p.usage != nil {
		p.result.Usage = *p.usage
		evs = append(evs, Event{Kind: EvUsage, Usage: p.usage})
	}
	return append(evs, Event{Kind: EvDone, Text: p.result.Text})
}

// scrub removes the API key from text that might echo it (an error body, a URL).
func scrub(s, key string) string {
	if len(key) >= 8 {
		s = strings.ReplaceAll(s, key, "[key]")
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func firstPositive(v ...int) int {
	for _, n := range v {
		if n > 0 {
			return n
		}
	}
	return 0
}
