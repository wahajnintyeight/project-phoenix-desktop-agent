// Package provider runs AI models through their vendors' own command-line clients.
//
// Phoenix never reads a vendor's credential files (Claude Code's .credentials.json and
// .claude.json, Codex's auth.json). Sign-in state is asked of the CLI itself, and the work
// is done by the CLI, which uses its own stored login. The alternative is an API key,
// which is handed to the CLI through its environment for that one process and never
// written anywhere by Phoenix except the encrypted vault.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
)

// ID names a provider.
type ID string

const (
	Claude ID = "claude"
	Codex  ID = "codex"
)

// AuthMode says how a provider authenticates.
type AuthMode string

const (
	// AuthCLI uses the CLI's own sign-in: a Claude subscription, a ChatGPT plan, or
	// whatever the user did with `claude auth login` / `codex login`.
	AuthCLI AuthMode = "cli"
	// AuthAPIKey uses an API key supplied by Phoenix.
	AuthAPIKey AuthMode = "api_key"
)

func (m AuthMode) Valid() bool { return m == AuthCLI || m == AuthAPIKey }

// Settings is what a provider needs to act. APIKey is already resolved from its vault or
// environment reference, so it is a secret: never log it, never send it to the UI.
type Settings struct {
	Mode    AuthMode
	APIKey  string
	Model   string // optional; empty means the CLI's own default
	Effort  string // optional reasoning effort; empty means the model's default
	CLIPath string // optional; searched for when empty
	BaseURL string // HTTP providers only; empty means the provider's default
}

// Model is one selectable model.
type Model struct {
	ID            string   `json:"id"` // exactly what is passed to the CLI (an alias or a full name)
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	ContextWindow int      `json:"contextWindow,omitempty"`
	Efforts       []string `json:"efforts,omitempty"` // reasoning-effort levels, lowest first
	DefaultEffort string   `json:"defaultEffort,omitempty"`
}

var (
	modelIDRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]-]{0,99}$`)
	effortRE    = regexp.MustCompile(`^[a-z]{3,10}$`)
	ErrBadModel = errors.New("that is not a valid model id")
)

// ValidModelID accepts "" (the CLI default) or a plausible model name. The first character
// must not be "-": the value becomes a command-line argument.
func ValidModelID(s string) bool { return s == "" || modelIDRE.MatchString(s) }

// ValidEffort accepts "" (the model default) or a lowercase level name such as "high".
func ValidEffort(s string) bool { return s == "" || effortRE.MatchString(s) }

// Login is the CLI's own sign-in state.
type Login struct {
	SignedIn bool   `json:"signedIn"`
	Method   string `json:"method,omitempty"`  // "claude.ai", "console", "chatgpt", "api-key"
	Plan     string `json:"plan,omitempty"`    // e.g. "max"
	Account  string `json:"account,omitempty"` // display only; never logged, never sent to a model
}

// Status is a snapshot for the UI. It carries no secrets.
type Status struct {
	ID         ID       `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"` // "cli" (a vendor command-line client) or "api" (HTTP, API key only)
	Installed  bool     `json:"installed"`
	CLIPath    string   `json:"cliPath,omitempty"`
	CLIVersion string   `json:"cliVersion,omitempty"`
	Login      Login    `json:"login"`
	Mode       AuthMode `json:"mode"`
	HasAPIKey  bool     `json:"hasApiKey"`
	KeySource  string   `json:"keySource,omitempty"` // "vault", "env", "literal"
	Ready      bool     `json:"ready"`
	Problem    string   `json:"problem,omitempty"` // why it is not ready, in words for the user

	// HTTP providers only.
	BaseURL        string `json:"baseUrl,omitempty"`
	DefaultBaseURL string `json:"defaultBaseUrl,omitempty"`
	KeyHelp        string `json:"keyHelp,omitempty"`
	BaseHint       string `json:"baseHint,omitempty"`
}

// EventKind classifies a streamed event.
type EventKind string

const (
	EvStart      EventKind = "start"      // session established
	EvTextDelta  EventKind = "text_delta" // part of the reply, as it arrives
	EvText       EventKind = "text"       // a complete reply message (when no deltas were sent)
	EvThinking   EventKind = "thinking"
	EvToolUse    EventKind = "tool_use"
	EvToolResult EventKind = "tool_result"
	EvUsage      EventKind = "usage"
	EvRateLimit  EventKind = "rate_limit"
	EvError      EventKind = "error"
	EvDone       EventKind = "done"
)

// Event is one normalized step of a run. Fields not relevant to Kind are zero.
type Event struct {
	Kind      EventKind       `json:"kind"`
	Text      string          `json:"text,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Model     string          `json:"model,omitempty"`
	ToolID    string          `json:"toolId,omitempty"`
	ToolName  string          `json:"toolName,omitempty"`
	ToolInput json.RawMessage `json:"toolInput,omitempty"`
	IsError   bool            `json:"isError,omitempty"`
	Usage     *Usage          `json:"usage,omitempty"`
	Limits    []Limit         `json:"limits,omitempty"`
}

// Usage is token accounting for a run, as far as the CLI reports it.
type Usage struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CachedTokens int     `json:"cachedTokens,omitempty"`
	CostUSD      float64 `json:"costUsd,omitempty"` // 0 when the CLI does not report cost
}

// Limit is a rate-limit window (Claude subscriptions report these).
type Limit struct {
	Window      string  `json:"window"`      // "five_hour", "seven_day"
	Utilization float64 `json:"utilization"` // 0..1
	ResetsAt    int64   `json:"resetsAt"`    // unix seconds
	Status      string  `json:"status"`
}

// Request is one run.
type Request struct {
	Prompt  string // sent on stdin, so length is not limited by the command line
	System  string // appended to the CLI's system prompt where the CLI supports it
	Model   string // overrides Settings.Model
	Effort  string // overrides Settings.Effort
	WorkDir string // the run's working root; an empty directory is created if blank

	// MCPConfig is the path of a JSON file of MCP servers (Phoenix's own tool server).
	MCPConfig    string
	AllowedTools []string // tool names the CLI may use without asking, e.g. "mcp__phoenix__*"
	MaxBudgetUSD float64  // 0 means no cap
}

// Result is the outcome of a run.
type Result struct {
	Text       string
	SessionID  string
	Usage      Usage
	IsError    bool
	DurationMS int64
}

// Provider is one vendor CLI.
type Provider interface {
	ID() ID
	// Detect reports installation and sign-in state. It runs the CLI, so it takes a moment.
	Detect(ctx context.Context, s Settings) Status
	// Models lists the models the user can pick. Any other valid model id is also accepted.
	Models(ctx context.Context, s Settings) ([]Model, error)
	// Login starts the CLI's interactive sign-in in its own console window and returns when
	// the user finishes or abandons it.
	Login(ctx context.Context, s Settings) error
	// Run streams one prompt through the CLI. emit is called from the calling goroutine.
	Run(ctx context.Context, s Settings, req Request, emit func(Event)) (Result, error)
}

var (
	ErrNotInstalled = errors.New("the command-line client is not installed")
	ErrNotReady     = errors.New("provider is not signed in or has no API key")
)
