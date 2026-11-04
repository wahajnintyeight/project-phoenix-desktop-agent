package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CodexProvider drives the OpenAI Codex CLI (`codex`).
type CodexProvider struct{}

func (CodexProvider) ID() ID { return Codex }

var codexAuthEnv = []string{"OPENAI_API_KEY", "CODEX_API_KEY"}

func codexPath(s Settings) (string, bool) {
	home, appData, local := userDirs()
	return lookCLI(s.CLIPath, "codex",
		filepath.Join(local, "Programs", "OpenAI", "Codex", "bin", "codex.exe"),
		filepath.Join(appData, "npm", "codex.cmd"),
		filepath.Join(home, ".local", "bin", "codex.exe"),
	)
}

// codexEnv: `codex exec` reads CODEX_API_KEY. Setting it per process means Phoenix never
// runs `codex login --with-api-key`, which would write the key into the user's own
// ~/.codex/auth.json.
func codexEnv(s Settings) []string {
	if s.Mode == AuthAPIKey && s.APIKey != "" {
		return cleanEnv(append(codexAuthEnv, "PHOENIX_CODEX_MCP_TOKEN"), "CODEX_API_KEY="+s.APIKey)
	}
	return cleanEnv(append(codexAuthEnv, "PHOENIX_CODEX_MCP_TOKEN"))
}

func (c CodexProvider) Detect(ctx context.Context, s Settings) Status {
	st := Status{ID: Codex, Name: "Codex", Kind: "cli", Mode: s.Mode, HasAPIKey: s.APIKey != ""}
	path, ok := codexPath(s)
	if !ok {
		st.Problem = "Codex is not installed. Install the Codex CLI, then check again."
		return st
	}
	st.Installed, st.CLIPath = true, path
	env := cleanEnv(codexAuthEnv)

	if v, err := capture(ctx, 15*time.Second, env, path, "--version"); err == nil {
		st.CLIVersion = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "codex-cli"))
	}
	// `codex login status` prints a sentence (to stdout or stderr depending on version),
	// and exits non-zero when signed out. Only the text is used.
	out, _ := captureBoth(ctx, 20*time.Second, env, path, "login", "status")
	st.Login = parseCodexLoginStatus(out)

	switch {
	case s.Mode == AuthAPIKey && s.APIKey == "":
		st.Problem = "No API key is saved for Codex."
	case s.Mode == AuthAPIKey:
		st.Ready = true
	case !st.Login.SignedIn:
		st.Problem = "Codex is not signed in. Use “Sign in with ChatGPT”, or switch to an API key."
	default:
		st.Ready = true
	}
	return st
}

// parseCodexLoginStatus understands "Logged in using ChatGPT", "Logged in using an API
// key" and "Not logged in".
func parseCodexLoginStatus(text string) Login {
	t := strings.ToLower(text)
	if !strings.Contains(t, "logged in") || strings.Contains(t, "not logged in") {
		return Login{}
	}
	l := Login{SignedIn: true}
	switch {
	case strings.Contains(t, "chatgpt"):
		l.Method = "chatgpt"
	case strings.Contains(t, "api key"):
		l.Method = "api-key"
	}
	return l
}

// Models reads the model catalog the installed Codex ships with (`codex debug models`), so
// the list follows the CLI version without Phoenix hard-coding names. Models Codex marks
// hidden are left out; they can still be typed in as a custom id.
func (c CodexProvider) Models(ctx context.Context, s Settings) ([]Model, error) {
	path, ok := codexPath(s)
	if !ok {
		return nil, ErrNotInstalled
	}
	out, err := capture(ctx, 30*time.Second, cleanEnv(codexAuthEnv), path, "debug", "models")
	if err != nil && out == "" {
		return nil, err
	}
	return parseCodexCatalog([]byte(out), s.Mode == AuthAPIKey)
}

func parseCodexCatalog(b []byte, apiKeyMode bool) ([]Model, error) {
	var cat struct {
		Models []struct {
			Slug         string `json:"slug"`
			DisplayName  string `json:"display_name"`
			Description  string `json:"description"`
			DefaultLevel string `json:"default_reasoning_level"`
			Levels       []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
			Visibility     string `json:"visibility"`
			SupportedInAPI *bool  `json:"supported_in_api"`
			ContextWindow  int    `json:"context_window"`
			Priority       int    `json:"priority"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &cat); err != nil {
		return nil, fmt.Errorf("could not read the Codex model catalog: %w", err)
	}
	sort.SliceStable(cat.Models, func(i, j int) bool { return cat.Models[i].Priority < cat.Models[j].Priority })

	out := []Model{{ID: "", Name: "CLI default", Description: "Whatever Codex chooses for your account"}}
	for _, m := range cat.Models {
		if m.Visibility != "list" || m.Slug == "" || !ValidModelID(m.Slug) {
			continue
		}
		if apiKeyMode && m.SupportedInAPI != nil && !*m.SupportedInAPI {
			continue
		}
		var efforts []string
		for _, l := range m.Levels {
			if ValidEffort(l.Effort) && l.Effort != "" {
				efforts = append(efforts, l.Effort)
			}
		}
		out = append(out, Model{
			ID: m.Slug, Name: firstNonEmpty(m.DisplayName, m.Slug), Description: m.Description,
			ContextWindow: m.ContextWindow, Efforts: efforts, DefaultEffort: m.DefaultLevel,
		})
	}
	return out, nil
}

func (c CodexProvider) Login(ctx context.Context, s Settings) error {
	path, ok := codexPath(s)
	if !ok {
		return ErrNotInstalled
	}
	return runInConsole(ctx, cleanEnv(codexAuthEnv), path, "login")
}

func (c CodexProvider) Run(ctx context.Context, s Settings, req Request, emit func(Event)) (Result, error) {
	path, ok := codexPath(s)
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

	prompt := req.Transcript()
	if req.System != "" {
		// codex exec has no system-prompt flag; instructions travel with the prompt.
		prompt = req.System + "\n\n" + prompt
	}
	parser := &codexParser{}
	env := codexEnv(s)
	if req.CodexMCPURL != "" {
		env = append(env, "PHOENIX_CODEX_MCP_TOKEN="+req.CodexMCPToken)
	}
	stderr, err := stream(ctx, env, dir, path, codexArgs(s, req, dir), prompt, func(line []byte) {
		for _, ev := range parser.Parse(line) {
			emit(ev)
		}
	})
	if err != nil && !parser.done {
		if ctx.Err() != nil {
			return parser.result, ctx.Err()
		}
		msg := firstNonEmpty(parser.lastError, lastLine(stderr), err.Error())
		emit(Event{Kind: EvError, Text: msg, IsError: true})
		return parser.result, fmt.Errorf("codex: %s", msg)
	}
	return parser.result, nil
}

// codexArgs: read-only sandbox, no persisted session, and neither the user's config.toml
// nor their execpolicy rules. Auth still comes from CODEX_HOME, so a ChatGPT login works.
// The prompt is read from stdin (the trailing "-").
func codexArgs(s Settings, req Request, dir string) []string {
	args := []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
		"-s", "read-only",
		"-C", dir,
	}
	if req.CodexMCPURL != "" {
		url, _ := json.Marshal(req.CodexMCPURL)
		args = append(args, "-c", `mcp_servers.phoenix={url=`+string(url)+`,bearer_token_env_var="PHOENIX_CODEX_MCP_TOKEN",tool_timeout_sec=360,default_tools_approval_mode="approve"}`)
	}
	if m := firstNonEmpty(req.Model, s.Model); m != "" {
		args = append(args, "-m", m)
	}
	if e := firstNonEmpty(req.Effort, s.Effort); e != "" {
		// The value is TOML; ValidEffort has already limited it to lowercase letters.
		args = append(args, "-c", `model_reasoning_effort="`+e+`"`)
	}
	return append(args, "-")
}

// plainError turns an API error body such as {"error":{"message":"..."}} into its message;
// anything else is returned unchanged.
func plainError(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return s
	}
	var e struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(s), &e) != nil {
		return s
	}
	return firstNonEmpty(e.Error.Message, e.Message, s)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// codexParser turns `codex exec --json` lines into Events. Codex reports whole items, not
// text deltas, so replies arrive as EvText.
type codexParser struct {
	done      bool
	lastError string
	result    Result
	text      []string
}

func (p *codexParser) Parse(line []byte) []Event {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &head) != nil {
		return nil
	}
	switch head.Type {
	case "thread.started":
		var m struct {
			ThreadID string `json:"thread_id"`
		}
		_ = json.Unmarshal(line, &m)
		p.result.SessionID = m.ThreadID
		return []Event{{Kind: EvStart, SessionID: m.ThreadID}}

	case "item.started", "item.completed", "item.updated":
		var m struct {
			Item struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal(line, &m) != nil {
			return nil
		}
		completed := head.Type == "item.completed"
		switch m.Item.Type {
		case "agent_message":
			if !completed {
				return nil
			}
			p.text = append(p.text, m.Item.Text)
			return []Event{{Kind: EvText, Text: m.Item.Text}}
		case "reasoning":
			if completed {
				return []Event{{Kind: EvThinking, Text: m.Item.Text}}
			}
			return nil
		case "error":
			if completed {
				p.lastError = m.Item.Text
			}
			return nil
		default: // command_execution, file_change, mcp_tool_call, web_search, ...
			var whole struct {
				Item json.RawMessage `json:"item"`
			}
			_ = json.Unmarshal(line, &whole)
			if completed {
				return []Event{{Kind: EvToolResult, ToolID: m.Item.ID, ToolName: m.Item.Type, ToolInput: whole.Item}}
			}
			if head.Type == "item.started" {
				return []Event{{Kind: EvToolUse, ToolID: m.Item.ID, ToolName: m.Item.Type, ToolInput: whole.Item}}
			}
		}
		return nil

	case "turn.completed":
		var m struct {
			Usage struct {
				In     int `json:"input_tokens"`
				Cached int `json:"cached_input_tokens"`
				Out    int `json:"output_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(line, &m)
		p.done = true
		p.result.Usage = Usage{InputTokens: m.Usage.In, OutputTokens: m.Usage.Out, CachedTokens: m.Usage.Cached}
		p.result.Text = strings.Join(p.text, "\n\n")
		return []Event{{Kind: EvUsage, Usage: &p.result.Usage}, {Kind: EvDone, Text: p.result.Text}}

	case "turn.failed", "error":
		var m struct {
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(line, &m)
		msg := plainError(firstNonEmpty(m.Error.Message, m.Message, "Codex reported an error"))
		p.lastError = msg
		if head.Type == "turn.failed" {
			p.done = true
			p.result.IsError = true
			return []Event{{Kind: EvError, Text: msg, IsError: true}, {Kind: EvDone, Text: msg, IsError: true}}
		}
		return []Event{{Kind: EvError, Text: msg, IsError: true}}
	}
	return nil
}
