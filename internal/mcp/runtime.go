package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxRuntimeTools     = 100
	maxToolSchema       = 64 << 10
	maxToolSchemasTotal = 128 << 10
	maxToolArgs         = 64 << 10
	maxToolResult       = 32 << 10
	maxToolImage        = 4 << 20
	maxToolImages       = 4
)

var ErrUserDeclined = errors.New("the user declined the tool call")

// Image is a safe image part from an MCP tool result.
type Image struct {
	MIMEType string
	Data     []byte
}

// ToolResult contains the bounded content to return to the provider loop.
type ToolResult struct {
	Text   string
	Images []Image
}

// Function is an MCP tool presented as a provider function. Name is a stable safe alias;
// Server and MCPName keep the original target private from provider-specific naming rules.
type Function struct {
	Name            string
	Server          string
	MCPName         string
	Description     string
	Parameters      json.RawMessage
	RequireApproval bool
}

type target struct {
	server  Resolved
	session *sdk.ClientSession
	tool    string
}

// Runtime holds MCP sessions open for one model run so discovery and function calls share
// the same authenticated connection. Close it when the provider run ends.
type Runtime struct {
	sessions  []*sdk.ClientSession
	cleanups  []func()
	functions []Function
	targets   map[string]target
}

func transport(s Resolved, oauthHandler auth.OAuthHandler) sdk.Transport {
	switch s.Transport {
	case Stdio:
		cmd := exec.Command(s.Command, s.Args...)
		cmd.Env = append(os.Environ(), envPairs(s.EnvValues)...)
		hideWindow(cmd)
		return &sdk.CommandTransport{Command: cmd, TerminateDuration: 3 * time.Second}
	case HTTP:
		return &sdk.StreamableClientTransport{
			Endpoint:     s.URL,
			OAuthHandler: oauthHandler,
			HTTPClient: &http.Client{
				Transport:     headerTransport{h: s.HeaderValues, next: http.DefaultTransport},
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			},
			MaxRetries: -1,
		}
	default:
		return nil
	}
}

func connect(ctx context.Context, s Resolved) (*sdk.ClientSession, func(), error) {
	if err := s.Validate(); err != nil {
		return nil, nil, err
	}
	cleanup := func() {}
	var oauthHandler auth.OAuthHandler
	if s.Transport == HTTP {
		var err error
		oauthHandler, cleanup, err = s.oauthHandler()
		if err != nil {
			return nil, nil, err
		}
	}
	tr := transport(s, oauthHandler)
	if tr == nil {
		cleanup()
		return nil, nil, errors.New("unknown MCP transport")
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "phoenix", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, tr, nil)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return session, cleanup, nil
}

// Open connects to all enabled servers and collects their functions. If any server is
// unavailable or publishes a schema that cannot safely be sent to a provider, it closes
// every session and fails the run instead of silently exposing a partial tool set.
func Open(ctx context.Context, servers []Resolved) (*Runtime, error) {
	r := &Runtime{targets: map[string]target{}}
	cleanup := func() { r.Close() }
	totalSchemaBytes := 0
	for _, server := range servers {
		session, cleanupSession, err := connect(ctx, server)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("MCP server %s: could not connect: %w", server.Name, scrub(err, server))
		}
		r.sessions = append(r.sessions, session)
		r.cleanups = append(r.cleanups, cleanupSession)
		for tool, err := range session.Tools(ctx, nil) {
			if err != nil {
				cleanup()
				return nil, fmt.Errorf("MCP server %s: could not list tools: %w", server.Name, scrub(err, server))
			}
			if len(r.functions) >= maxRuntimeTools {
				cleanup()
				return nil, fmt.Errorf("enabled MCP servers offer more than %d tools; disable some servers", maxRuntimeTools)
			}
			schema, err := inputSchema(tool.InputSchema)
			if err != nil {
				cleanup()
				return nil, fmt.Errorf("MCP server %s tool %s: %w", server.Name, tool.Name, err)
			}
			totalSchemaBytes += len(schema)
			if totalSchemaBytes > maxToolSchemasTotal {
				cleanup()
				return nil, errors.New("enabled MCP tool schemas are too large; disable some servers")
			}
			schema = json.RawMessage(redactValues(string(schema), server))
			name := fmt.Sprintf("mcp_tool_%03d", len(r.functions)+1)
			fn := Function{
				Name: name, Server: server.Name, MCPName: tool.Name,
				Description: clip(redactValues("MCP server "+server.Name+" tool "+tool.Name+": "+tool.Description, server), 800),
				Parameters:  schema, RequireApproval: server.RequireApproval,
			}
			r.functions = append(r.functions, fn)
			r.targets[name] = target{server: server, session: session, tool: tool.Name}
		}
	}
	return r, nil
}

func inputSchema(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) > maxToolSchema {
		return nil, errors.New("the input schema is invalid or too large")
	}
	var schema map[string]json.RawMessage
	if json.Unmarshal(b, &schema) != nil || schema == nil {
		return nil, errors.New("the input schema must be a JSON object")
	}
	return b, nil
}

// Functions returns a copy of the callable tools for the model request.
func (r *Runtime) Functions() []Function { return append([]Function(nil), r.functions...) }

// Call invokes only a function advertised by this runtime and bounds both arguments and
// returned content. Configured secrets are scrubbed before any result reaches the model.
func (r *Runtime) Call(ctx context.Context, name string, arguments json.RawMessage) (ToolResult, error) {
	t, ok := r.targets[name]
	if !ok {
		return ToolResult{}, errors.New("that tool is not available in this run")
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if len(arguments) > maxToolArgs {
		return ToolResult{}, errors.New("tool arguments are too large")
	}
	var args map[string]any
	if json.Unmarshal(arguments, &args) != nil || args == nil {
		return ToolResult{}, errors.New("tool arguments must be a JSON object")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, err := t.session.CallTool(ctx, &sdk.CallToolParams{Name: t.tool, Arguments: args})
	if err != nil {
		return ToolResult{}, scrub(err, t.server)
	}
	out := resultOutput(result, t.server)
	if result != nil && result.IsError && strings.Contains(strings.ToLower(out.Text), "do not try it again") {
		return ToolResult{}, ErrUserDeclined
	}
	return out, nil
}

func resultOutput(result *sdk.CallToolResult, server Resolved) ToolResult {
	if result == nil {
		return ToolResult{Text: "The tool returned no result."}
	}
	var out ToolResult
	var parts []string
	if result.StructuredContent != nil {
		if b, err := json.Marshal(result.StructuredContent); err == nil {
			parts = append(parts, string(b))
		}
	}
	imageBytes := 0
	for _, content := range result.Content {
		switch c := content.(type) {
		case *sdk.TextContent:
			parts = append(parts, c.Text)
		case *sdk.ImageContent:
			if imageType(c.MIMEType) && len(out.Images) < maxToolImages && len(c.Data) <= maxToolImage && imageBytes+len(c.Data) <= maxToolImage {
				out.Images = append(out.Images, Image{MIMEType: c.MIMEType, Data: append([]byte(nil), c.Data...)})
				imageBytes += len(c.Data)
			} else {
				parts = append(parts, "[image result omitted: unsupported type or too large]")
			}
		default:
			parts = append(parts, "[non-text tool content omitted]")
		}
	}
	out.Text = redactValues(strings.Join(parts, "\n"), server)
	if result.IsError {
		out.Text = "Tool error: " + out.Text
	}
	out.Text = clip(out.Text, maxToolResult)
	return out
}

func imageType(mime string) bool {
	switch strings.ToLower(mime) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func (r *Runtime) Close() {
	for _, session := range r.sessions {
		_ = session.Close()
	}
	r.sessions = nil
	for _, cleanup := range r.cleanups {
		cleanup()
	}
	r.cleanups = nil
}
