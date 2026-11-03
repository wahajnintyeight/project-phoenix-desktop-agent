package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeProvider drives the Claude Code CLI (`claude`).
type ClaudeProvider struct{}

func (ClaudeProvider) ID() ID { return Claude }

// Variables that would make the CLI authenticate differently from the mode the user chose.
var claudeAuthEnv = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"}

func claudePath(s Settings) (string, bool) {
	home, appData, _ := userDirs()
	return lookCLI(s.CLIPath, "claude",
		filepath.Join(home, ".local", "bin", "claude.exe"),
		filepath.Join(home, ".claude", "local", "claude.exe"),
		filepath.Join(appData, "npm", "claude.cmd"),
	)
}

// claudeEnv builds the child environment for the chosen mode. In CLI mode any API key in
// the environment is removed so the subscription login is what gets used; in API-key mode
// the key is set for this process only.
func claudeEnv(s Settings) []string {
	if s.Mode == AuthAPIKey && s.APIKey != "" {
		return cleanEnv(claudeAuthEnv, "ANTHROPIC_API_KEY="+s.APIKey)
	}
	return cleanEnv(claudeAuthEnv)
}

func (c ClaudeProvider) Detect(ctx context.Context, s Settings) Status {
	st := Status{ID: Claude, Name: "Claude", Kind: "cli", Mode: s.Mode, HasAPIKey: s.APIKey != ""}
	path, ok := claudePath(s)
	if !ok {
		st.Problem = "Claude Code is not installed. Install it from claude.com/claude-code, then check again."
		return st
	}
	st.Installed, st.CLIPath = true, path
	env := cleanEnv(claudeAuthEnv) // login state is a property of the CLI, not of any key

	if v, err := capture(ctx, 15*time.Second, env, path, "--version"); err == nil {
		st.CLIVersion = strings.TrimSpace(v)
	}
	if out, err := capture(ctx, 20*time.Second, env, path, "auth", "status", "--json"); err == nil || out != "" {
		st.Login = parseClaudeAuthStatus([]byte(out))
	}

	switch {
	case s.Mode == AuthAPIKey && s.APIKey == "":
		st.Problem = "No API key is saved for Claude."
	case s.Mode == AuthAPIKey:
		st.Ready = true
	case !st.Login.SignedIn:
		st.Problem = "Claude Code is not signed in. Use “Sign in with Claude”, or switch to an API key."
	default:
		st.Ready = true
	}
	return st
}

// parseClaudeAuthStatus reads the JSON `claude auth status` prints. Fields it does not
// know are ignored; missing ones leave the zero value, which reads as "not signed in".
func parseClaudeAuthStatus(b []byte) Login {
	var j struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		SubscriptionType string `json:"subscriptionType"`
		Email            string `json:"email"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return Login{}
	}
	return Login{SignedIn: j.LoggedIn, Method: j.AuthMethod, Plan: j.SubscriptionType, Account: j.Email}
}

// Claude Code has no command that lists models, so these are its documented aliases. An
// alias always means the latest model of that family; the concrete model that answered is
// reported by the run itself (the start event). Any full model name is accepted too.
// Descriptions are Phoenix's own short labels, not text from Anthropic.
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"} // from `claude --help`

func (ClaudeProvider) Models(_ context.Context, _ Settings) ([]Model, error) {
	m := func(id, name, desc string) Model {
		return Model{ID: id, Name: name, Description: desc, Efforts: claudeEfforts}
	}
	return []Model{
		m("", "CLI default", "Whatever Claude Code chooses for your account"),
		m("fable", "Fable", "Latest Fable model"),
		m("opus", "Opus", "Latest Opus model, for complex work"),
		m("sonnet", "Sonnet", "Latest Sonnet model, balanced speed and capability"),
		m("haiku", "Haiku", "Latest Haiku model, fastest and lowest cost"),
	}, nil
}

func (c ClaudeProvider) Login(ctx context.Context, s Settings) error {
	path, ok := claudePath(s)
	if !ok {
		return ErrNotInstalled
	}
	return runInConsole(ctx, cleanEnv(claudeAuthEnv), path, "auth", "login", "--claudeai")
}

func (c ClaudeProvider) Run(ctx context.Context, s Settings, req Request, emit func(Event)) (Result, error) {
	path, ok := claudePath(s)
	if !ok {
		return Result{}, ErrNotInstalled
	}
	if s.Mode == AuthAPIKey && s.APIKey == "" {
		return Result{}, ErrNotReady
	}
	if err := checkModelArgs(s, req); err != nil {
		return Result{}, err
	}
	dir, cleanup, err := workDir(req.WorkDir)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	args := claudeArgs(s, req)
	res, parser := Result{}, &claudeParser{}
	stderr, err := stream(ctx, claudeEnv(s), dir, path, args, req.Transcript(), func(line []byte) {
		for _, ev := range parser.Parse(line) {
			if ev.Kind == EvDone {
				res = parser.result
			}
			emit(ev)
		}
	})
	if err != nil && !parser.done {
		if ctx.Err() != nil {
			return parser.result, ctx.Err()
		}
		msg := tail(stderr, 400)
		if msg == "" {
			msg = err.Error()
		}
		emit(Event{Kind: EvError, Text: msg, IsError: true})
		return parser.result, fmt.Errorf("claude: %s", msg)
	}
	return res, nil
}

// claudeArgs is the full isolation recipe. Phoenix's agent gets exactly the tools it passes
// in, never Claude Code's own shell and file tools, and none of the user's hooks, skills,
// plugins' settings, memory files or MCP servers: those belong to the user's coding setup.
func claudeArgs(s Settings, req Request) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--no-session-persistence",
		"--disable-slash-commands",
		"--setting-sources=", // no user, project or local settings (and so no hooks)
		"--tools=",           // no built-in tools
		"--strict-mcp-config",
	}
	if req.MCPConfig != "" {
		args = append(args, "--mcp-config", req.MCPConfig)
	}
	if len(req.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(req.AllowedTools, ","))
	}
	if m := firstNonEmpty(req.Model, s.Model); m != "" {
		args = append(args, "--model", m)
	}
	if e := firstNonEmpty(req.Effort, s.Effort); e != "" {
		args = append(args, "--effort", e)
	}
	if req.System != "" {
		args = append(args, "--append-system-prompt", req.System)
	}
	if req.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.4f", req.MaxBudgetUSD))
	}
	return args
}

// checkModelArgs refuses values that could be read as a command-line flag or are otherwise
// not a model name or effort level. The model and effort become process arguments.
func checkModelArgs(s Settings, req Request) error {
	if !ValidModelID(firstNonEmpty(req.Model, s.Model)) {
		return ErrBadModel
	}
	if !ValidEffort(firstNonEmpty(req.Effort, s.Effort)) {
		return errors.New("that is not a valid effort level")
	}
	return nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// claudeParser turns Claude Code's stream-json lines into Events.
type claudeParser struct {
	sawDelta bool // text for the current message already streamed as deltas
	done     bool
	result   Result
}

func (p *claudeParser) Parse(line []byte) []Event {
	var head struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
	}
	if json.Unmarshal(line, &head) != nil {
		return nil
	}
	switch head.Type {
	case "system":
		if head.Subtype != "init" {
			return nil
		}
		var m struct {
			SessionID string `json:"session_id"`
			Model     string `json:"model"`
		}
		_ = json.Unmarshal(line, &m)
		p.result.SessionID = m.SessionID
		return []Event{{Kind: EvStart, SessionID: m.SessionID, Model: m.Model}}

	case "stream_event":
		var m struct {
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					Thinking string `json:"thinking"`
				} `json:"delta"`
			} `json:"event"`
		}
		if json.Unmarshal(line, &m) != nil {
			return nil
		}
		switch m.Event.Type {
		case "message_start":
			p.sawDelta = false
		case "content_block_delta":
			switch m.Event.Delta.Type {
			case "text_delta":
				p.sawDelta = true
				return []Event{{Kind: EvTextDelta, Text: m.Event.Delta.Text}}
			case "thinking_delta":
				return []Event{{Kind: EvThinking, Text: m.Event.Delta.Thinking}}
			}
		}
		return nil

	case "assistant":
		var m struct {
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Text  string          `json:"text"`
					ID    string          `json:"id"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &m) != nil {
			return nil
		}
		var out []Event
		for _, b := range m.Message.Content {
			switch b.Type {
			case "text":
				if !p.sawDelta && b.Text != "" {
					out = append(out, Event{Kind: EvText, Text: b.Text})
				}
			case "tool_use":
				out = append(out, Event{Kind: EvToolUse, ToolID: b.ID, ToolName: b.Name, ToolInput: b.Input})
			}
		}
		return out

	case "user":
		var m struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &m) != nil {
			return nil
		}
		var blocks []struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
			IsError   bool            `json:"is_error"`
		}
		if json.Unmarshal(m.Message.Content, &blocks) != nil {
			return nil // plain-string content is a user prompt echo
		}
		var out []Event
		for _, b := range blocks {
			if b.Type == "tool_result" {
				out = append(out, Event{Kind: EvToolResult, ToolID: b.ToolUseID, Text: flattenContent(b.Content), IsError: b.IsError})
			}
		}
		return out

	case "rate_limit_event":
		var m struct {
			Info struct {
				Windows map[string]struct {
					Utilization float64 `json:"utilization"`
					ResetsAt    int64   `json:"resetsAt"`
					Status      string  `json:"status"`
				} `json:"unifiedWindows"`
			} `json:"rate_limit_info"`
		}
		if json.Unmarshal(line, &m) != nil || len(m.Info.Windows) == 0 {
			return nil
		}
		var limits []Limit
		for _, name := range []string{"five_hour", "seven_day"} { // fixed order for stable UI
			if w, ok := m.Info.Windows[name]; ok {
				limits = append(limits, Limit{Window: name, Utilization: w.Utilization, ResetsAt: w.ResetsAt, Status: w.Status})
			}
		}
		return []Event{{Kind: EvRateLimit, Limits: limits}}

	case "result":
		var m struct {
			IsError    bool    `json:"is_error"`
			Result     string  `json:"result"`
			SessionID  string  `json:"session_id"`
			DurationMS int64   `json:"duration_ms"`
			CostUSD    float64 `json:"total_cost_usd"`
			Usage      struct {
				In    int `json:"input_tokens"`
				Out   int `json:"output_tokens"`
				Cache int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &m) != nil {
			return nil
		}
		p.done = true
		p.result = Result{
			Text:       m.Result,
			SessionID:  firstNonEmpty(m.SessionID, p.result.SessionID),
			IsError:    m.IsError,
			DurationMS: m.DurationMS,
			Usage:      Usage{InputTokens: m.Usage.In, OutputTokens: m.Usage.Out, CachedTokens: m.Usage.Cache, CostUSD: m.CostUSD},
		}
		out := []Event{{Kind: EvUsage, Usage: &p.result.Usage}}
		if m.IsError {
			out = append(out, Event{Kind: EvError, Text: m.Result, IsError: true})
		}
		return append(out, Event{Kind: EvDone, Text: m.Result, IsError: m.IsError})
	}
	return nil
}

// flattenContent turns a tool_result content (a string, or a list of text blocks) into text.
func flattenContent(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, x := range blocks {
			b.WriteString(x.Text)
		}
		return b.String()
	}
	return string(raw)
}
