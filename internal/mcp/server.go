// Package mcp lets Phoenix use Model Context Protocol servers: the user installs several,
// Phoenix keeps them in config.yaml (secrets in the vault), can connect to one to list its
// tools, and hands the enabled ones to the agent.
//
// A server entry names a program to run (stdio) or a URL to call (http), so adding one is a
// code-execution decision. Nothing the model, a web page or a tool result says can add or
// change an entry; only the Settings UI can, and it shows the exact command first.
package mcp

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Transport is how Phoenix reaches a server.
type Transport string

const (
	Stdio Transport = "stdio" // Phoenix starts the program and talks over its stdin and stdout
	HTTP  Transport = "http"  // Streamable HTTP at a URL
)

// Server is one installed MCP server as stored in config.yaml. Env and Headers hold
// references ("vault:<name>"), never the secret values themselves.
type Server struct {
	Name      string            `yaml:"-" json:"name"` // the key in config.yaml
	Transport Transport         `yaml:"transport" json:"transport"`
	Command   string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env       map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	URL       string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	// Enabled servers are offered to the agent. A server that is installed but not enabled is
	// only ever started by the Test button.
	Enabled bool `yaml:"enabled" json:"enabled"`
}

const (
	maxServers = 24
	maxArgs    = 64
	maxArgLen  = 2000
	maxPairs   = 24
)

var (
	// The name becomes part of every tool name (mcp__<name>__<tool>), so it stays short and
	// free of the "__" separator.
	nameRE   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)
	envRE    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	headerRE = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,64}$")
)

// ValidName reports whether s can name a server.
func ValidName(s string) bool { return nameRE.MatchString(s) }

// ValidURL accepts https, or http only for this machine, with no login in the address.
// Headers (often an access token) are sent there, so it must be an address the user chose.
func ValidURL(s string) bool {
	if len(s) > 500 {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	}
	return false
}

// Validate checks a server before it is saved or run. The command and arguments become a
// process, so control characters are refused.
func (s Server) Validate() error {
	if !ValidName(s.Name) {
		return errors.New("the name must be 1 to 32 lowercase letters, digits or hyphens")
	}
	switch s.Transport {
	case Stdio:
		if strings.TrimSpace(s.Command) == "" || len(s.Command) > 500 || hasControl(s.Command) {
			return errors.New("the command is missing or not valid")
		}
		if len(s.Args) > maxArgs {
			return errors.New("too many arguments")
		}
		for _, a := range s.Args {
			if len(a) > maxArgLen || hasControl(a) {
				return errors.New("an argument is too long or has control characters")
			}
		}
		if s.URL != "" || len(s.Headers) > 0 {
			return errors.New("a command server has no URL or headers")
		}
	case HTTP:
		if !ValidURL(s.URL) {
			return errors.New("the address must start with https:// and have no login (http is allowed only for localhost)")
		}
		if s.Command != "" || len(s.Args) > 0 || len(s.Env) > 0 {
			return errors.New("a URL server has no command, arguments or environment")
		}
	default:
		return fmt.Errorf("unknown transport %q", s.Transport)
	}
	if len(s.Env) > maxPairs || len(s.Headers) > maxPairs {
		return errors.New("too many environment variables or headers")
	}
	for k := range s.Env {
		if !envRE.MatchString(k) {
			return fmt.Errorf("%q is not a valid environment variable name", k)
		}
	}
	for k := range s.Headers {
		if !headerRE.MatchString(k) {
			return fmt.Errorf("%q is not a valid header name", k)
		}
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// Describe is the one-line summary shown before a server is installed: exactly what would
// run or be called.
func (s Server) Describe() string {
	if s.Transport == HTTP {
		return s.URL
	}
	return strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
}

// CleanMap drops invalid entries (and any beyond the limit) from the map config.yaml holds,
// and sets each entry's Name from its key, so the rest of the app can trust what is left.
func CleanMap(in map[string]Server) map[string]Server {
	names := make([]string, 0, len(in))
	for k := range in {
		names = append(names, k)
	}
	sort.Strings(names)
	out := map[string]Server{}
	for _, k := range names {
		s := in[k]
		s.Name = k
		if len(out) >= maxServers || s.Validate() != nil {
			continue
		}
		out[k] = s
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// List returns the servers sorted by name.
func List(m map[string]Server) []Server {
	out := make([]Server, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
