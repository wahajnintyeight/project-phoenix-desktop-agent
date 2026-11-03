package main

import "golang.org/x/sys/windows"

// The few Win32 calls left now that Wails owns the window: reading the window's DPI and
// state, and ordering the child windows (the UI webview and the tab pages) inside it.

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	pIsZoomed        = user32.NewProc("IsZoomed")
	pSetWindowPos    = user32.NewProc("SetWindowPos")
	pGetWindow       = user32.NewProc("GetWindow")
	pGetDpiForWindow = user32.NewProc("GetDpiForWindow")
	pSetFocus        = user32.NewProc("SetFocus")
)

const (
	gwChild       = 5
	gwHwndNext    = 2
	hwndBottom    = 1
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
)

func isZoomed(hwnd uintptr) bool {
	r, _, _ := pIsZoomed.Call(hwnd)
	return r != 0
}

func dpiFor(hwnd uintptr) uint32 {
	r, _, _ := pGetDpiForWindow.Call(hwnd)
	if r == 0 {
		return 96
	}
	return uint32(r)
}

// childWindows lists the immediate child windows of the Wails window, top first.
func childWindows() []uintptr {
	var out []uintptr
	h, _, _ := pGetWindow.Call(hwnd, gwChild)
	for h != 0 {
		out = append(out, h)
		h, _, _ = pGetWindow.Call(h, gwHwndNext)
	}
	return out
}
