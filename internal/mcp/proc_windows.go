package mcp

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a console window from flashing up when a server is started.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
