package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func startRelaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--phoenix-relaunch", strconv.Itoa(os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return cmd.Start()
}

func runRelaunch(parentID string) int {
	pid, err := strconv.ParseUint(parentID, 10, 32)
	if err != nil || pid == 0 || uint32(pid) == uint32(os.Getpid()) {
		return 1
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil && err != windows.ERROR_INVALID_PARAMETER {
		return 1
	}
	if err == nil {
		result, waitErr := windows.WaitForSingleObject(handle, windows.INFINITE)
		windows.CloseHandle(handle)
		if waitErr != nil || result != windows.WAIT_OBJECT_0 {
			return 1
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return 1
	}
	cmd := exec.Command(exe)
	cmd.Env = relaunchEnv(os.Environ())
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "phoenix restart:", err)
		return 1
	}
	return 0
}

func relaunchEnv(env []string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "FRONTEND_DEVSERVER_URL", "WAILS_INTERNAL_DEV_READY_URL", "WAILS_INTERNAL_DEV_READY_TOKEN":
			continue
		}
		result = append(result, entry)
	}
	return result
}
