package provider

import (
	"strings"
	"testing"
)

func kinds(evs []Event) []EventKind {
	k := make([]EventKind, len(evs))
	for i, e := range evs {
		k[i] = e.Kind
	}
	return k
}

func feed(p interface{ Parse([]byte) []Event }, lines ...string) []Event {
	var all []Event
	for _, l := range lines {
		all = append(all, p.Parse([]byte(l))...)
	}
	return all
}

// Lines below follow what Claude Code 2.1.316 printed for `-p --output-format stream-json
// --verbose --include-partial-messages`, trimmed to the fields Phoenix reads.
const (
	claudeInit  = `{"type":"system","subtype":"init","session_id":"s-1","model":"claude-haiku-5-5","tools":[]}`
	claudeStart = `{"type":"stream_event","event":{"type":"message_start"}}`
	claudeDelta = `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}}`
	claudeMsg   = `{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`
	claudeDone  = `{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"s-1","duration_ms":1200,"total_cost_usd":0.0007,"usage":{"input_tokens":2,"output_tokens":4,"cache_read_input_tokens":649}}`
)

func TestClaudeStreamWithDeltas(t *testing.T) {
	p := &claudeParser{}
	evs := feed(p, claudeInit, `{"type":"system","subtype":"status"}`, claudeStart, claudeDelta, claudeMsg, claudeDone)
	got := kinds(evs)
	want := []EventKind{EvStart, EvTextDelta, EvUsage, EvDone}
	if strings.Join(toStr(got), ",") != strings.Join(toStr(want), ",") {
		t.Fatalf("kinds = %v, want %v (the assistant message must not repeat text already streamed)", got, want)
	}
	if evs[0].SessionID != "s-1" || evs[0].Model != "claude-haiku-5-5" {
		t.Errorf("start event: %+v", evs[0])
	}
	r := p.result
	if r.Text != "ok" || r.SessionID != "s-1" || r.IsError || r.Usage.OutputTokens != 4 || r.Usage.CachedTokens != 649 || r.Usage.CostUSD != 0.0007 {
		t.Errorf("result: %+v", r)
	}
}

func TestClaudeStreamWithoutDeltasEmitsText(t *testing.T) {
	evs := feed(&claudeParser{}, claudeInit, claudeStart, claudeMsg, claudeDone)
	if got := kinds(evs); len(got) < 2 || got[1] != EvText || evs[1].Text != "ok" {
		t.Fatalf("expected a full-text event, got %v", got)
	}
}

func TestClaudeToolUseAndResult(t *testing.T) {
	// Synthetic: shaped after the documented stream-json message format, not captured from
	// a real tool run (the live check ran with all tools disabled).
	evs := feed(&claudeParser{},
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__phoenix__snapshot","input":{"tab":2}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"3 elements"}],"is_error":false}]}}`,
		`{"type":"user","message":{"content":"a plain prompt echo"}}`,
	)
	if len(evs) != 2 || evs[0].Kind != EvToolUse || evs[0].ToolName != "mcp__phoenix__snapshot" || string(evs[0].ToolInput) != `{"tab":2}` {
		t.Fatalf("tool_use: %+v", evs)
	}
	if evs[1].Kind != EvToolResult || evs[1].ToolID != "t1" || evs[1].Text != "3 elements" {
		t.Fatalf("tool_result: %+v", evs[1])
	}
}

func TestClaudeRateLimit(t *testing.T) {
	evs := feed(&claudeParser{}, `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","unifiedWindows":{"seven_day":{"utilization":0.91,"resetsAt":1793858400,"status":"allowed_warning"},"five_hour":{"utilization":0.3,"resetsAt":1793722800,"status":"allowed"}}}}`)
	if len(evs) != 1 || evs[0].Kind != EvRateLimit || len(evs[0].Limits) != 2 {
		t.Fatalf("%+v", evs)
	}
	if evs[0].Limits[0].Window != "five_hour" || evs[0].Limits[1].Utilization != 0.91 {
		t.Errorf("limits not in the fixed order or wrong: %+v", evs[0].Limits)
	}
}

func TestClaudeErrorResult(t *testing.T) {
	p := &claudeParser{}
	evs := feed(p, `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Credit balance is too low","usage":{}}`)
	got := kinds(evs)
	if len(got) != 3 || got[1] != EvError || got[2] != EvDone || !evs[2].IsError || !p.result.IsError {
		t.Fatalf("%v %+v", got, p.result)
	}
}

func TestClaudeIgnoresGarbage(t *testing.T) {
	if evs := feed(&claudeParser{}, `not json`, `{}`, `{"type":"unknown"}`, `{"type":"stream_event","event":`); len(evs) != 0 {
		t.Fatalf("garbage produced events: %+v", evs)
	}
}

func TestParseClaudeAuthStatus(t *testing.T) {
	in := parseClaudeAuthStatus([]byte(`{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max","email":"a@b.c","orgId":"x"}`))
	if !in.SignedIn || in.Method != "claude.ai" || in.Plan != "max" || in.Account != "a@b.c" {
		t.Errorf("%+v", in)
	}
	for _, s := range []string{`{"loggedIn":false}`, ``, `Not logged in`, `{`} {
		if parseClaudeAuthStatus([]byte(s)).SignedIn {
			t.Errorf("%q read as signed in", s)
		}
	}
}

func TestCodexStream(t *testing.T) {
	p := &codexParser{}
	evs := feed(p,
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"id":"i1","type":"command_execution","command":"dir"}}`,
		`{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"dir","exit_code":0}}`,
		`{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":"ok"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":17915,"cached_input_tokens":12416,"output_tokens":5}}`,
	)
	want := "start,tool_use,tool_result,text,usage,done"
	if got := strings.Join(toStr(kinds(evs)), ","); got != want {
		t.Fatalf("kinds = %s, want %s", got, want)
	}
	if p.result.Text != "ok" || p.result.SessionID != "th-1" || p.result.Usage.InputTokens != 17915 || p.result.Usage.CachedTokens != 12416 {
		t.Errorf("result: %+v", p.result)
	}
}

func TestCodexFailure(t *testing.T) {
	p := &codexParser{}
	evs := feed(p, `{"type":"turn.failed","error":{"message":"usage limit reached"}}`)
	if len(evs) != 2 || evs[0].Kind != EvError || evs[0].Text != "usage limit reached" || !p.result.IsError || !p.done {
		t.Fatalf("%+v", evs)
	}
}

func TestParseCodexLoginStatus(t *testing.T) {
	for in, want := range map[string]Login{
		"Logged in using ChatGPT":        {SignedIn: true, Method: "chatgpt"},
		"Logged in using an API key":     {SignedIn: true, Method: "api-key"},
		"Not logged in":                  {},
		"not logged in. Run codex login": {},
		"":                               {},
	} {
		if got := parseCodexLoginStatus(in); got != want {
			t.Errorf("%q → %+v, want %+v", in, got, want)
		}
	}
}

const fakeKey = "sk-ant-FAKE-KEY-FOR-TESTS"

func TestAPIKeyNeverOnCommandLine(t *testing.T) {
	s := Settings{Mode: AuthAPIKey, APIKey: fakeKey, Model: "m"}
	req := Request{Prompt: "hi", System: "sys", MCPConfig: "x.json", AllowedTools: []string{"a"}}
	for _, args := range [][]string{claudeArgs(s, req), codexArgs(s, req, `C:\tmp`)} {
		if strings.Contains(strings.Join(args, " "), fakeKey) {
			t.Fatalf("the API key appears in the command line: %v", args)
		}
	}
}

func TestClaudeIsolationFlags(t *testing.T) {
	args := strings.Join(claudeArgs(Settings{}, Request{Prompt: "x"}), " ")
	for _, must := range []string{"--setting-sources=", "--tools=", "--strict-mcp-config", "--no-session-persistence", "--disable-slash-commands", "stream-json"} {
		if !strings.Contains(args, must) {
			t.Errorf("missing %s in %s", must, args)
		}
	}
	if strings.Contains(args, "--dangerously-skip-permissions") {
		t.Error("permissions must never be bypassed")
	}
}

func TestCodexIsolationFlags(t *testing.T) {
	args := codexArgs(Settings{}, Request{}, `C:\tmp`)
	joined := strings.Join(args, " ")
	for _, must := range []string{"--json", "--ephemeral", "--ignore-user-config", "--ignore-rules", "-s read-only"} {
		if !strings.Contains(joined, must) {
			t.Errorf("missing %s in %s", must, joined)
		}
	}
	if strings.Contains(joined, "danger") || args[len(args)-1] != "-" {
		t.Errorf("unsafe or stdin-less args: %v", args)
	}
}

func envValue(env []string, name string) (string, int) {
	var v string
	n := 0
	for _, kv := range env {
		if k, val, _ := strings.Cut(kv, "="); strings.EqualFold(k, name) {
			v, n = val, n+1
		}
	}
	return v, n
}

func TestEnvDependsOnMode(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ambient-key")
	t.Setenv("OPENAI_API_KEY", "ambient-openai")

	// CLI mode: an ambient key must not override the sign-in the user chose.
	if _, n := envValue(claudeEnv(Settings{Mode: AuthCLI}), "ANTHROPIC_API_KEY"); n != 0 {
		t.Error("ANTHROPIC_API_KEY leaked into CLI mode")
	}
	if _, n := envValue(codexEnv(Settings{Mode: AuthCLI}), "OPENAI_API_KEY"); n != 0 {
		t.Error("OPENAI_API_KEY leaked into CLI mode")
	}
	// API mode: exactly the configured key, once, replacing the ambient one.
	if v, n := envValue(claudeEnv(Settings{Mode: AuthAPIKey, APIKey: fakeKey}), "ANTHROPIC_API_KEY"); v != fakeKey || n != 1 {
		t.Errorf("claude API mode: %q ×%d", v, n)
	}
	if v, n := envValue(codexEnv(Settings{Mode: AuthAPIKey, APIKey: fakeKey}), "CODEX_API_KEY"); v != fakeKey || n != 1 {
		t.Errorf("codex API mode: %q ×%d", v, n)
	}
}

func toStr(k []EventKind) []string {
	s := make([]string, len(k))
	for i, x := range k {
		s[i] = string(x)
	}
	return s
}

func TestEffortAndModelFlags(t *testing.T) {
	s := Settings{Model: "opus", Effort: "high"}
	c := strings.Join(claudeArgs(s, Request{}), " ")
	if !strings.Contains(c, "--model opus") || !strings.Contains(c, "--effort high") {
		t.Errorf("claude: %s", c)
	}
	// a request overrides the saved setting
	c = strings.Join(claudeArgs(s, Request{Model: "haiku", Effort: "low"}), " ")
	if !strings.Contains(c, "--model haiku") || !strings.Contains(c, "--effort low") {
		t.Errorf("claude override: %s", c)
	}
	x := codexArgs(Settings{Model: "m1", Effort: "medium"}, Request{}, `C:\tmp`)
	j := strings.Join(x, "\x00")
	if !strings.Contains(j, "-m\x00m1") || !strings.Contains(j, "-c\x00model_reasoning_effort=\"medium\"") {
		t.Errorf("codex: %q", x)
	}
	if x[len(x)-1] != "-" {
		t.Error("prompt must still be read from stdin")
	}
	if strings.Contains(strings.Join(claudeArgs(Settings{}, Request{}), " "), "--effort") {
		t.Error("effort flag sent when none chosen")
	}
}

func TestRunRefusesFlagLookingModelBeforeStartingAnything(t *testing.T) {
	for _, bad := range []Request{{Model: "--dangerously-skip-permissions"}, {Model: "a b"}, {Effort: "High"}, {Effort: `x"y`}} {
		if checkModelArgs(Settings{}, bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if checkModelArgs(Settings{Model: "claude-opus-4-1", Effort: "xhigh"}, Request{}) != nil {
		t.Error("rejected a normal model")
	}
}

const codexCatalog = `{"models":[
 {"slug":"hidden-one","display_name":"Hidden","visibility":"hide","priority":1},
 {"slug":"b-model","display_name":"B","description":"second","visibility":"list","priority":20,"context_window":128000,
  "default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"}],"supported_in_api":true},
 {"slug":"a-model","display_name":"A","visibility":"list","priority":2,"supported_in_api":false,
  "supported_reasoning_levels":[{"effort":"high"}]},
 {"slug":"--evil","display_name":"Evil","visibility":"list","priority":3}
]}`

func TestParseCodexCatalog(t *testing.T) {
	ms, err := parseCodexCatalog([]byte(codexCatalog), false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if got := strings.Join(ids, ","); got != ",a-model,b-model" { // default entry, then by priority; hidden and invalid dropped
		t.Fatalf("ids = %q", got)
	}
	b := ms[2]
	if b.Name != "B" || b.ContextWindow != 128000 || b.DefaultEffort != "medium" || strings.Join(b.Efforts, ",") != "low,medium,high" {
		t.Errorf("%+v", b)
	}
	api, _ := parseCodexCatalog([]byte(codexCatalog), true)
	if len(api) != 2 || api[1].ID != "b-model" {
		t.Errorf("api-key mode should hide models not supported in the API: %+v", api)
	}
	if _, err := parseCodexCatalog([]byte("not json"), false); err == nil {
		t.Error("garbage accepted")
	}
}

func TestClaudeModelsListsDefaultFirst(t *testing.T) {
	ms, _ := ClaudeProvider{}.Models(nil, Settings{})
	if len(ms) < 2 || ms[0].ID != "" {
		t.Fatalf("%+v", ms)
	}
	for _, m := range ms {
		if !ValidModelID(m.ID) || len(m.Efforts) == 0 {
			t.Errorf("%+v", m)
		}
	}
}

func TestCodexAPIErrorBodyIsUnwrapped(t *testing.T) {
	p := &codexParser{}
	evs := p.Parse([]byte(`{"type":"error","message":"{\"type\":\"error\",\"status\":400,\"error\":{\"message\":\"The 'x' model is not supported.\",\"code\":\"model_not_found\"}}"}`))
	if len(evs) != 1 || evs[0].Text != "The 'x' model is not supported." {
		t.Fatalf("%+v", evs)
	}
	if plainError("plain words") != "plain words" || plainError("{broken") != "{broken" {
		t.Error("non-JSON text must pass through")
	}
}
func TestCodexToolProxyConfigKeepsTokenOutOfArguments(t *testing.T) {
	req := Request{CodexMCPURL: "http://127.0.0.1:4321/mcp", CodexMCPToken: "secret-token"}
	args := strings.Join(codexArgs(Settings{}, req, `C:\tmp`), " ")
	if !strings.Contains(args, "mcp_servers.phoenix=") || !strings.Contains(args, "PHOENIX_CODEX_MCP_TOKEN") || !strings.Contains(args, `default_tools_approval_mode="approve"`) || strings.Contains(args, req.CodexMCPToken) {
		t.Fatalf("invalid Codex tool configuration: %s", args)
	}
}

func TestCodexArgsDisableBuiltInFeatures(t *testing.T) {
	args := strings.Join(codexArgs(Settings{}, Request{CodexDisable: []string{"shell_tool", "plugins"}}, "."), " ")
	if !strings.Contains(args, "--disable shell_tool") || !strings.Contains(args, "--disable plugins") {
		t.Fatalf("args = %s", args)
	}
	if strings.Contains(strings.Join(codexArgs(Settings{}, Request{}, "."), " "), "--disable") {
		t.Fatal("disable flags without a request for them")
	}
}
