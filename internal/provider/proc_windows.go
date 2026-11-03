package provider

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	createNoWindow   = 0x08000000
	createNewConsole = 0x00000010
)

// hideWindow keeps a console window from flashing up for a background run.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// killTree stops the process and everything it started (tool subprocesses included).
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	k := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	k.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := k.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

// runInConsole runs an interactive command in its own visible console window and waits for
// it. Sign-in flows need one: they may print a URL or ask for a code.
func runInConsole(ctx context.Context, env []string, path string, args ...string) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	return cmd.Run()
}
