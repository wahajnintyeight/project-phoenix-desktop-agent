//go:build windows

package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procIsIconic            = user32.NewProc("IsIconic")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowText       = user32.NewProc("GetWindowTextW")
)

type nativeWindow struct {
	App       string `json:"app_name"`
	Title     string `json:"title"`
	ID        string `json:"id"`
	Focused   bool   `json:"is_focused"`
	Minimized bool   `json:"minimized"`
	Visible   bool   `json:"visible"`
	X         int32  `json:"x"`
	Y         int32  `json:"y"`
	Width     int32  `json:"width"`
	Height    int32  `json:"height"`
}

func nativeWindows(ctx context.Context) ([]nativeWindow, error) {
	out := make([]nativeWindow, 0, 32)
	visited := 0
	foreground, _, _ := procGetForegroundWindow.Call()
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		visited++
		if ctx.Err() != nil || len(out) >= 120 || visited > 500 {
			return 0
		}
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		var title [512]uint16
		n, _, _ := procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
		if n == 0 {
			return 1
		}
		var pid uint32
		_, _ = windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
		if pid == 0 {
			return 1
		}
		process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			return 1
		}
		var exe [1024]uint16
		size := uint32(len(exe))
		err = windows.QueryFullProcessImageName(process, 0, &exe[0], &size)
		_ = windows.CloseHandle(process)
		if err != nil {
			return 1
		}
		app := filepath.Base(windows.UTF16ToString(exe[:size]))
		if checkApp(app) != nil {
			return 1
		}
		minimized, _, _ := procIsIconic.Call(hwnd)
		var bounds windowRect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
		out = append(out, nativeWindow{App: app, Title: windows.UTF16ToString(title[:n]), ID: fmt.Sprintf("w-%d", hwnd), Focused: hwnd == foreground, Minimized: minimized != 0, Visible: true, X: bounds.Left, Y: bounds.Top, Width: bounds.Right - bounds.Left, Height: bounds.Bottom - bounds.Top})
		return 1
	})
	result, _, callErr := procEnumWindows.Call(cb, 0)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if result == 0 && len(out) < 120 && visited <= 500 {
		return nil, fmt.Errorf("enumerating windows failed: %w", callErr)
	}
	return out, nil
}

func selectNativeWindow(windowsList []nativeWindow, app, id string) (uintptr, error) {
	var match *nativeWindow
	for i := range windowsList {
		w := &windowsList[i]
		if !strings.EqualFold(w.App, app) || id != "" && w.ID != id {
			continue
		}
		if match != nil {
			return 0, errors.New("several windows match this app; choose a window_id from desktop_windows")
		}
		match = w
	}
	if match == nil {
		return 0, errors.New("the selected window is no longer available; list windows again")
	}
	if match.Minimized {
		return 0, errors.New("restore the minimized window before capturing it")
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(match.ID, "w-"), 10, 64)
	if err != nil {
		return 0, errors.New("invalid window id")
	}
	return uintptr(n), nil
}

func nativeEnvelope(command string, v any) (Envelope, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{OK: true, Command: command, Data: data}, nil
}

func runNative(ctx context.Context, args []string) (Envelope, bool, error) {
	if len(args) == 0 {
		return Envelope{}, false, nil
	}
	switch args[0] {
	case "list-windows":
		ws, err := nativeWindows(ctx)
		if err != nil {
			return Envelope{}, true, err
		}
		env, err := nativeEnvelope(args[0], ws)
		return env, true, err
	case "screenshot":
		var app, id, path string
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--app":
				if i+1 < len(args) {
					i++
					app = args[i]
				}
			case "--window-id":
				if i+1 < len(args) {
					i++
					id = args[i]
				}
			case "--":
				if i+1 < len(args) {
					path = args[i+1]
					i = len(args)
				}
			}
		}
		if err := checkApp(app); err != nil {
			return Envelope{}, true, err
		}
		if err := checkWindow(id); err != nil {
			return Envelope{}, true, err
		}
		if path == "" {
			return Envelope{}, true, errors.New("screenshot destination is missing")
		}
		ws, err := nativeWindows(ctx)
		if err != nil {
			return Envelope{}, true, err
		}
		hwnd, err := selectNativeWindow(ws, app, id)
		if err != nil {
			return Envelope{}, true, err
		}
		png, err := captureNativeWindow(ctx, hwnd)
		if err != nil {
			return Envelope{}, true, err
		}
		if err = os.WriteFile(path, png, 0600); err != nil {
			return Envelope{}, true, err
		}
		env, err := nativeEnvelope(args[0], map[string]string{"format": "png"})
		return env, true, err
	case "clipboard-get":
		value, err := nativeClipboardRead()
		if err != nil {
			return Envelope{}, true, err
		}
		env, err := nativeEnvelope(args[0], map[string]string{"text": value})
		return env, true, err
	case "clipboard-set":
		if len(args) != 3 || args[1] != "--" {
			return Envelope{}, true, errors.New("invalid clipboard text")
		}
		if err := nativeClipboardWrite(args[2]); err != nil {
			return Envelope{}, true, err
		}
		env, err := nativeEnvelope(args[0], map[string]any{})
		return env, true, err
	}
	return Envelope{}, false, nil
}
