package provider

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// lookCLI finds a vendor CLI: an explicit path, then PATH, then the places the vendors'
// installers use.
func lookCLI(explicit, name string, extra ...string) (string, bool) {
	if explicit != "" {
		if st, err := os.Stat(explicit); err == nil && !st.IsDir() {
			return explicit, true
		}
		return "", false
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	for _, p := range extra {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

func userDirs() (home, appData, localAppData string) {
	home, _ = os.UserHomeDir()
	return home, os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")
}

// cleanEnv returns the process environment without the named variables, plus extra
// KEY=VALUE pairs. Matching is case-insensitive, as Windows environment names are.
func cleanEnv(drop []string, extra ...string) []string {
	skip := make(map[string]bool, len(drop))
	for _, d := range drop {
		skip[strings.ToUpper(d)] = true
	}
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !skip[strings.ToUpper(name)] {
			out = append(out, kv)
		}
	}
	return append(out, extra...)
}

// capture runs a short command and returns its combined stdout. It never shows a window.
func capture(ctx context.Context, timeout time.Duration, env []string, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Stdin = nil
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s timed out", filepath.Base(path))
	}
	out := stdout.String()
	if err != nil && out == "" {
		return "", fmt.Errorf("%s: %w: %s", filepath.Base(path), err, tail(stderr.String(), 300))
	}
	return out, err
}

// captureBoth is capture with stdout and stderr combined, for CLIs that print status to
// either. A non-zero exit is not an error here: the text is what matters.
func captureBoth(ctx context.Context, timeout time.Duration, env []string, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s timed out", filepath.Base(path))
	}
	return string(out), err
}

// stream runs a command, feeds it stdin, and calls onLine for each stdout line. It returns
// the last stderr bytes for error reporting. Cancelling ctx kills the whole process tree.
func stream(ctx context.Context, env []string, dir, path string, args []string, stdin string, onLine func([]byte)) (stderrTail string, err error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	hideWindow(cmd)
	cmd.Cancel = func() error { return killTree(cmd) }
	cmd.WaitDelay = 3 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr tailBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), 32<<20) // a tool result can be a very long line
	for sc.Scan() {
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 {
			onLine(line)
		}
	}
	scanErr := sc.Err()
	waitErr := cmd.Wait()
	switch {
	case ctx.Err() != nil:
		return stderr.String(), ctx.Err()
	case scanErr != nil && !errors.Is(scanErr, io.EOF):
		return stderr.String(), scanErr
	}
	return stderr.String(), waitErr
}

// tailBuffer keeps only the last few KB written to it.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 8<<10 {
		t.b = t.b[len(t.b)-8<<10:]
	}
	return len(p), nil
}
func (t *tailBuffer) String() string { return string(t.b) }

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

// workDir returns the directory to run in: the requested one, or a fresh empty one so a
// model never starts inside the user's project by accident.
// The returned cleanup removes a directory this function created, and does nothing for one
// the caller supplied.
func workDir(requested string) (dir string, cleanup func(), err error) {
	if requested != "" {
		return requested, func() {}, nil
	}
	base := filepath.Join(os.TempDir(), "phoenix-run")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", nil, err
	}
	dir, err = os.MkdirTemp(base, "r-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}
