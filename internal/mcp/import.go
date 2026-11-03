package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Candidate is a server read from pasted JSON, not yet installed. Env and Headers hold the
// real values; they go to the vault only when the user confirms.
type Candidate struct {
	Server
	EnvValues    map[string]string
	HeaderValues map[string]string
}

const maxImportBytes = 64 << 10

// ParseImport reads the JSON most MCP servers publish for their install instructions:
//
//	{"mcpServers": {"name": {"command": "npx", "args": ["-y", "pkg"], "env": {"K": "v"}}}}
//
// A bare map of name to entry, and {"servers": {...}}, are accepted too. Entries with a
// "url" are Streamable HTTP; entries with a "command" are programs. Names are made
// lowercase with hyphens. Everything is validated; the first problem is returned, so nothing
// is half-imported.
func ParseImport(text string) ([]Candidate, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("paste the server's JSON first")
	}
	if len(text) > maxImportBytes {
		return nil, errors.New("that is too large to be a server definition")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &top); err != nil {
		return nil, errors.New("that is not valid JSON")
	}
	entries := top
	for _, k := range []string{"mcpServers", "servers"} {
		if raw, ok := top[k]; ok {
			entries = nil
			if err := json.Unmarshal(raw, &entries); err != nil {
				return nil, fmt.Errorf("%q should map names to server definitions", k)
			}
			break
		}
	}
	if len(entries) == 0 {
		return nil, errors.New("no servers found in that JSON")
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []Candidate
	seen := map[string]bool{}
	for _, key := range keys {
		var e struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		}
		if err := json.Unmarshal(entries[key], &e); err != nil {
			return nil, fmt.Errorf("%s: not a server definition (command and args are text, env is name to text)", key)
		}
		name := NormalizeName(key)
		if seen[name] {
			return nil, fmt.Errorf("%s: two servers would get the name %q", key, name)
		}
		seen[name] = true
		c := Candidate{Server: Server{Name: name}, EnvValues: e.Env, HeaderValues: e.Headers}
		switch t := strings.ToLower(e.Type); {
		case t == "sse":
			return nil, fmt.Errorf("%s: SSE servers are not supported; use the server's Streamable HTTP address", key)
		case e.URL != "" && e.Command == "":
			c.Transport, c.URL = HTTP, e.URL
		case e.Command != "" && e.URL == "":
			c.Transport, c.Command, c.Args = Stdio, e.Command, e.Args
		default:
			return nil, fmt.Errorf("%s: needs either a command or a url", key)
		}
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out = append(out, c)
	}
	return out, nil
}

// NormalizeName makes a valid server name from free text: lowercase letters, digits and
// single hyphens, at most 32 characters. It can return an invalid (empty) name, which
// Validate then rejects.
func NormalizeName(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 32 {
		out = strings.TrimRight(out[:32], "-")
	}
	return out
}
