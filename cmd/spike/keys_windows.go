package main

import (
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// Browser shortcuts. WebView2 reports accelerator keys (anything with Ctrl, Alt, or a
// function key) to the host before the page sees them, so the same handler is attached
// to the UI controller and to every page controller. go-webview2's own
// AcceleratorKeyCallback can't be used: it calls put_Handled(false) after the callback
// returns, so a handled key would still reach the page.

var pGetKeyState = windows.NewLazySystemDLL("user32.dll").NewProc("GetKeyState")

func keyDown(vk uintptr) bool {
	s, _, _ := pGetKeyState.Call(vk)
	return int16(s) < 0
}

// ICoreWebView2Controller::add_AcceleratorKeyPressed (IDL order: 3 IUnknown + 16).
const ctrlAddAcceleratorKeyPressed = 19

// ICoreWebView2AcceleratorKeyPressedEventArgs slots.
const (
	accGetKeyEventKind = 3
	accGetVirtualKey   = 4
	accGetLParam       = 5
	accPutHandled      = 8
)

const (
	keyKindDown       = 0
	keyKindSystemDown = 2
)

const (
	vkTab      = 0x09
	vkPrior    = 0x21 // Page Up
	vkNext     = 0x22 // Page Down
	vkLeft     = 0x25
	vkRight    = 0x27
	vkF4       = 0x73
	vkF5       = 0x74
	vkF6       = 0x75
	vkF12      = 0x7B
	vkOemComma = 0xBC
	vkShift    = 0x10
	vkCtrl     = 0x11
	vkAlt      = 0x12
	repeatLP   = 1 << 30 // lParam bit 30: key was already down (auto-repeat)
)

func attachShortcuts(ctrl *edge.ICoreWebView2Controller) {
	h := newHandler(func(_, args unsafe.Pointer) {
		var kind int32
		comCall(args, accGetKeyEventKind, uintptr(unsafe.Pointer(&kind)))
		if kind != keyKindDown && kind != keyKindSystemDown {
			return
		}
		var vk uint32
		var lp int32
		comCall(args, accGetVirtualKey, uintptr(unsafe.Pointer(&vk)))
		comCall(args, accGetLParam, uintptr(unsafe.Pointer(&lp)))

		action := shortcutFor(vk)
		if action == nil {
			return
		}
		comCall(args, accPutHandled, 1) // keep it from the page, including auto-repeats
		if lp&repeatLP == 0 {
			// Never act inside the callback: closing or creating a controller from its own
			// key event is unsafe.
			runLater(action)
		}
	})
	var token int64
	if hr := comCall(unsafe.Pointer(ctrl), ctrlAddAcceleratorKeyPressed, uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&token))); hrErr("add AcceleratorKeyPressed", hr) != nil {
		logf("%v", hrErr("add AcceleratorKeyPressed", hr))
	}
}

// shortcutFor maps the pressed key plus current modifiers to an action, or nil if the key
// is not ours and should reach the page.
func shortcutFor(vk uint32) func() {
	ctrl, shift, alt := keyDown(vkCtrl), keyDown(vkShift), keyDown(vkAlt)

	switch {
	case alt && !ctrl && vk == vkLeft:
		return func() { navAction("nav.back") }
	case alt && !ctrl && vk == vkRight:
		return func() { navAction("nav.forward") }
	case alt && !ctrl && vk == 'D', !alt && !ctrl && vk == vkF6, ctrl && !alt && vk == 'L':
		return focusOmnibox
	case !alt && !ctrl && vk == vkF5, ctrl && !alt && vk == 'R':
		return func() { navAction("nav.reload") }
	case !alt && (!ctrl && vk == vkF12 || ctrl && shift && vk == 'I'):
		return openDevTools
	}

	if !ctrl || alt {
		return nil
	}
	switch {
	case vk == 'T' && !shift:
		return newTab
	case vk == vkOemComma && !shift:
		return openSettings
	case vk == 'W' && !shift, vk == vkF4:
		return func() { closeTab(activeID) }
	case vk == vkTab && !shift, vk == vkNext:
		return func() { cycleTab(+1) }
	case vk == vkTab && shift, vk == vkPrior:
		return func() { cycleTab(-1) }
	case vk >= '1' && vk <= '9' && !shift:
		n := int(vk - '1')
		return func() { selectNth(n, vk == '9') }
	}
	return nil
}

func focusOmnibox() {
	chromium.Focus() // OS keyboard focus to the UI webview so typing reaches the address bar
	post("f" + fs + "omnibox")
}

func newTab() {
	createWebTab("about:blank")
	focusOmnibox()
}

func cycleTab(dir int) {
	i, _ := findTab(activeID)
	if i < 0 || len(tabs) < 2 {
		return
	}
	activate(tabs[(i+dir+len(tabs))%len(tabs)].id)
}

// selectNth selects the nth tab, or the last one for Ctrl+9 (as other browsers do).
func selectNth(n int, last bool) {
	if last {
		n = len(tabs) - 1
	}
	if n >= 0 && n < len(tabs) {
		activate(tabs[n].id)
	}
}

func openDevTools() {
	if t := activeTab(); t != nil && t.cv != nil {
		t.cv.OpenDevToolsWindow()
	}
}
