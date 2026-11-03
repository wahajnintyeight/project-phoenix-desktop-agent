package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Resolver turns a stored reference ("vault:<name>", "env:<NAME>") into its value.
type Resolver func(ref string) (string, error)

// Resolved is a server with its secret references replaced by values. It exists only for
// the moment a server is started or described to a CLI, and is never logged or saved.
type Resolved struct {
	Server
	EnvValues    map[string]string
	HeaderValues map[string]string
}

// Resolve fills in a server's secrets.
func Resolve(s Server, r Resolver) (Resolved, error) {
	out := Resolved{Server: s, EnvValues: map[string]string{}, HeaderValues: map[string]string{}}
	for k, ref := range s.Env {
		v, err := r(ref)
		if err != nil {
			return Resolved{}, fmt.Errorf("%s: %w", k, err)
		}
		out.EnvValues[k] = v
	}
	for k, ref := range s.Headers {
		v, err := r(ref)
		if err != nil {
			return Resolved{}, fmt.Errorf("%s: %w", k, err)
		}
		out.HeaderValues[k] = v
	}
	return out, nil
}

// Tool is one tool a server offers, for display.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Info is what connecting to a server revealed.
type Info struct {
	Name    string `json:"name"`    // what the server calls itself
	Version string `json:"version"` //
	Tools   []Tool `json:"tools"`
}

const (
	maxTools    = 500
	maxDescLen  = 300
	testTimeout = 45 * time.Second
)

// Test connects to a server, reads its name and tool list, and disconnects. A stdio server
// is started for this and stopped again. Everything in the answer comes from the server, so
// the UI shows it as plain text only.
func Test(ctx context.Context, s Resolved) (Info, error) {
	if err := s.Validate(); err != nil {
		return Info{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	var tr sdk.Transport
	switch s.Transport {
	case Stdio:
		cmd := exec.Command(s.Command, s.Args...)
		cmd.Env = append(os.Environ(), envPairs(s.EnvValues)...)
		hideWindow(cmd)
		tr = &sdk.CommandTransport{Command: cmd, TerminateDuration: 3 * time.Second}
	case HTTP:
		tr = &sdk.StreamableClientTransport{
			Endpoint:   s.URL,
			HTTPClient: &http.Client{Transport: headerTransport{h: s.HeaderValues, next: http.DefaultTransport}},
			MaxRetries: -1,
		}
	}

	client := sdk.NewClient(&sdk.Implementation{Name: "phoenix", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, tr, nil)
	if err != nil {
		return Info{}, fmt.Errorf("could not connect: %w", scrub(err, s))
	}
	defer session.Close()

	info := Info{}
	if r := session.InitializeResult(); r != nil && r.ServerInfo != nil {
		info.Name, info.Version = clip(r.ServerInfo.Name, 80), clip(r.ServerInfo.Version, 40)
	}
	for t, err := range session.Tools(ctx, nil) {
		if err != nil {
			if len(info.Tools) == 0 {
				return info, fmt.Errorf("connected, but listing tools failed: %w", scrub(err, s))
			}
			break
		}
		if len(info.Tools) >= maxTools {
			break
		}
		info.Tools = append(info.Tools, Tool{Name: clip(t.Name, 100), Description: clip(t.Description, maxDescLen)})
	}
	sort.Slice(info.Tools, func(i, j int) bool { return info.Tools[i].Name < info.Tools[j].Name })
	return info, nil
}

// headerTransport adds the user's configured headers to every request.
type headerTransport struct {
	h    map[string]string
	next http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range t.h {
		r.Header.Set(k, v)
	}
	return t.next.RoundTrip(r)
}

func envPairs(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

// scrub removes secret values from an error before it reaches the screen or the log.
func scrub(err error, s Resolved) error {
	msg := err.Error()
	for _, m := range []map[string]string{s.EnvValues, s.HeaderValues} {
		for _, v := range m {
			if len(v) >= 4 {
				msg = strings.ReplaceAll(msg, v, "•••")
			}
		}
	}
	return errors.New(clip(msg, 400))
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

// ClaudeConfig renders servers in the format `claude --mcp-config` reads. It holds secret
// values, so the caller writes it to a private file and removes it after the run.
func ClaudeConfig(servers []Resolved) ([]byte, error) {
	type entry struct {
		Type    string            `json:"type"`
		Command string            `json:"command,omitempty"`
		Args    []string          `json:"args,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
		URL     string            `json:"url,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
	}
	m := map[string]entry{}
	for _, s := range servers {
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", s.Name, err)
		}
		switch s.Transport {
		case Stdio:
			m[s.Name] = entry{Type: "stdio", Command: s.Command, Args: s.Args, Env: nonEmpty(s.EnvValues)}
		case HTTP:
			m[s.Name] = entry{Type: "http", URL: s.URL, Headers: nonEmpty(s.HeaderValues)}
		}
	}
	return json.Marshal(map[string]any{"mcpServers": m})
}

func nonEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

// ClaudeAllowed lists the tool patterns the Claude CLI may run without asking: every tool
// of every server passed in. Claude's headless mode cannot ask, so a tool that is not here
// is refused.
func ClaudeAllowed(servers []Resolved) []string {
	out := make([]string, 0, len(servers))
	for _, s := range servers {
		out = append(out, "mcp__"+s.Name)
	}
	return out
}
