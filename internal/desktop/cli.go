// Package desktop gives the agent hands: it looks at windows, clicks, scrolls, drags, types,
// copies and pastes, by driving the agent-desktop command-line tool (Windows UI Automation)
// and exposing a small, curated set of tools as an MCP server.
//
// Seeing the screen or acting on it is the most sensitive thing Phoenix can do, so:
//   - the agent never gets a raw shell, only these named tools, with checked arguments;
//   - every tool asks the user first through an Approver (looking is asked once per app and
//     turn, acting is asked every time unless the user allowed all actions for the turn);
//   - no whole-screen capture: a screenshot always names an application;
//   - the CLI is started without a shell and never with its --force flag, so its own list of
//     blocked shortcuts (Alt+F4, Win+R, ...) stays in effect.
package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Find locates the agent-desktop program: the configured path, then PATH, then where npm
// puts a global install. It returns the native executable, never the .cmd launcher, so the
// program is started without a shell and arguments are never re-parsed.
func Find(explicit string) (string, bool) {
	exe := "agent-desktop-win32-" + map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH] + ".exe"
	var candidates []string
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if p, err := exec.LookPath("agent-desktop.exe"); err == nil {
		candidates = append(candidates, p)
	}
	var npmDirs []string
	if p, err := exec.LookPath("agent-desktop.cmd"); err == nil {
		npmDirs = append(npmDirs, filepath.Dir(p))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		npmDirs = append(npmDirs, filepath.Join(appData, "npm"))
	}
	for _, d := range npmDirs {
		candidates = append(candidates, filepath.Join(d, "node_modules", "agent-desktop", "bin", exe))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && strings.HasSuffix(strings.ToLower(c), ".exe") {
			return c, true
		}
	}
	return "", false
}

// CLI runs the agent-desktop program.
type CLI struct{ Path string }

// Envelope is the one JSON object every agent-desktop command prints.
type Envelope struct {
	OK      bool            `json:"ok"`
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		Suggestion string `json:"suggestion"`
	} `json:"error"`
}

// Err turns a failed envelope into an error whose text the model can act on.
func (e Envelope) Err() error {
	if e.OK {
		return nil
	}
	if e.Error == nil {
		return errors.New("the desktop tool failed")
	}
	msg := e.Error.Code + ": " + e.Error.Message
	if e.Error.Suggestion != "" {
		msg += " Suggestion: " + e.Error.Suggestion
	}
	return errors.New(msg)
}

const maxOutput = 8 << 20

// Run executes one command. Arguments go straight to the process, so they are not
// interpreted by any shell. headed asks for real input (cursor, focus) instead of the
// accessibility API.
func (c CLI) Run(ctx context.Context, headed bool, args ...string) (Envelope, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	full := args
	if headed {
		full = append([]string{"--headed"}, args...)
	}
	cmd := exec.CommandContext(ctx, c.Path, full...)
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, n: maxOutput}
	cmd.Stderr = &limitWriter{w: &stderr, n: 4096}
	runErr := cmd.Run()
	var env Envelope
	if jerr := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &env); jerr != nil {
		if ctx.Err() != nil {
			return Envelope{}, errors.New("the desktop tool took too long")
		}
		if runErr != nil {
			return Envelope{}, fmt.Errorf("the desktop tool failed to run: %v", runErr)
		}
		return Envelope{}, errors.New("the desktop tool printed something unexpected")
	}
	return env, nil
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if len(p) > l.n {
		p = p[:l.n]
	}
	if l.n > 0 {
		l.n -= len(p)
		_, _ = l.w.Write(p)
	}
	return len(p), nil // never fail the process for output we chose to drop
}

// Version asks the program for its version, which also proves it runs.
func (c CLI) Version(ctx context.Context) (string, error) {
	env, err := c.Run(ctx, false, "version")
	if err != nil {
		return "", err
	}
	if err := env.Err(); err != nil {
		return "", err
	}
	var d struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(env.Data, &d)
	return d.Version, nil
}
