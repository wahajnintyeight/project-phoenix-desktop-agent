// Command spike is the window-chrome prototype: a borderless Win32 window hosting a
// WebView2 that renders Phoenix's own UI, with a message round trip to Go.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"

	"phoenix/internal/ui"
)

const (
	fs = "\u001f" // field separator
	rs = "\u001e" // record separator
)

var (
	hwnd     uintptr
	chromium *edge.Chromium
	core     *edge.ICoreWebView2
	ready    bool
	logFile  *os.File
)

// logf writes diagnostics to %TEMP%\phoenix-spike.log when PHOENIX_LOG=1.
func logf(format string, args ...any) {
	if logFile == nil {
		return
	}
	fmt.Fprintf(logFile, format+"\n", args...)
}

func main() {
	runtime.LockOSThread()

	if os.Getenv("PHOENIX_LOG") == "1" {
		logFile, _ = os.Create(filepath.Join(os.TempDir(), "phoenix-spike.log"))
	}

	// Per-monitor DPI awareness v2 (-4).
	pSetProcessDpiAwarenessContext.Call(^uintptr(3))

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "spike:", err)
		os.Exit(1)
	}
}

func run() error {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	brush, _, _ := pCreateSolidBrush.Call(rgb(0x15, 0x17, 0x1a)) // --bg-1

	className := utf16Ptr("PhoenixSpikeWindow")
	wc := wndClassEx{
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		hCursor:       cursor,
		hbrBackground: brush,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}

	var err error
	h, _, e := pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("Phoenix"))),
		wsOverlappedWindow|wsClipChildren|wsClipSiblings,
		cwUseDefault, cwUseDefault, 1280, 800,
		0, 0, hInst, 0,
	)
	if h == 0 {
		return fmt.Errorf("CreateWindowEx: %w", e)
	}
	hwnd = h

	// A 1px extended frame at the top keeps the DWM shadow and border on a frameless window.
	m := margins{top: 1}
	pDwmExtendFrameIntoClientArea.Call(hwnd, uintptr(unsafe.Pointer(&m)))
	// Force a WM_NCCALCSIZE pass so the frame is recomputed with our handler.
	pSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)

	if err = startWebView(); err != nil {
		return err
	}

	var m2 msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m2)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m2)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m2)))
	}
	return nil
}

func startWebView() error {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return fmt.Errorf("LOCALAPPDATA is not set")
	}
	// Every WebView2 controller in the process must use the same data folder and the same
	// browser arguments, or the runtime refuses to create the second environment.
	browserDataPath = filepath.Join(local, "Phoenix", "spike", "browser")
	// Dev-only: expose a DevTools port on loopback so scripts can inspect the UI and pages.
	// Never enable this in a release build; any local process could drive the browser.
	if port := os.Getenv("PHOENIX_DEBUG_PORT"); port != "" {
		browserArgs = append(browserArgs,
			"--remote-debugging-port="+port, "--remote-debugging-address=127.0.0.1")
	}
	debugBuild = os.Getenv("PHOENIX_DEBUG") == "1"

	chromium = edge.NewChromium()
	chromium.DataPath = browserDataPath
	chromium.AdditionalBrowserArgs = browserArgs
	chromium.MessageCallback = onMessage
	chromium.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		logf("navigation completed")
		if b, err := chromium.GetController().GetBounds(); err == nil {
			logf("controller bounds: %d,%d %d,%d", b.Left, b.Top, b.Right, b.Bottom)
		}
		pShowWindow.Call(hwnd, swShow)
		// The controller was created while the parent was hidden, so WebView2 still
		// reports the page as hidden (no frames). Make it visible explicitly.
		_ = chromium.Show()
		_ = chromium.GetController().NotifyParentWindowPositionChanged()
		chromium.Focus()
		pSetForegroundWindow.Call(hwnd)
	}
	chromium.SetErrorCallback(func(err error) {
		fmt.Fprintln(os.Stderr, "webview2:", err)
		logf("ui webview2: %v\n%s", err, debug.Stack())
	})

	if !chromium.Embed(hwnd) {
		return fmt.Errorf("WebView2 embed failed")
	}
	// The UI webview is the only child so far. Page webviews must always draw over it, but
	// resizing the UI controller raises its window, so the order is re-asserted by lowerUI.
	uiView, _, _ = pGetWindow.Call(hwnd, gwChild)
	lowerUI()
	updatePadding()

	settings, err := chromium.GetSettings()
	if err != nil {
		return fmt.Errorf("GetSettings: %w", err)
	}
	_ = settings.PutAreDevToolsEnabled(debugBuild)
	_ = settings.PutAreDefaultContextMenusEnabled(debugBuild)
	_ = settings.PutIsStatusBarEnabled(false)
	_ = settings.PutIsZoomControlEnabled(false)
	// The UI is not a web page: its own Ctrl+R, F5, F12 and friends must not apply to it.
	_ = settings.PutAreBrowserAcceleratorKeysEnabled(debugBuild)
	attachShortcuts(chromium.GetController())
	_ = settings.PutAreDefaultScriptDialogsEnabled(true)

	ncr, ncrErr := enableNonClientRegions(unsafe.Pointer(settings))
	envInfo.ncr = fmt.Sprintf("enabled=%v", ncr)
	if ncrErr != nil {
		envInfo.ncr = "error: " + ncrErr.Error()
	}

	ctrl := chromium.GetController()
	core, err = ctrl.GetCoreWebView2()
	if err != nil {
		return fmt.Errorf("GetCoreWebView2: %w", err)
	}

	initTabs()
	initProviders()
	chromium.Resize()
	chromium.NavigateToString(ui.IndexHTML)
	ready = true
	return nil
}

var envInfo struct{ ncr string }

// onMessage handles UI → Go messages. It runs on the UI thread.
func onMessage(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
	f := strings.Split(message, fs)
	switch f[0] {
	case "p", "i":
	case "g":
		// Chat text is private: log the operation only.
		if len(f) > 1 {
			logf("msg g %s", f[1])
		}
	case "v":
		// Provider messages can carry an API key: log the operation, never the fields.
		if len(f) > 2 {
			logf("msg v %s %s", f[1], f[2])
		}
	default:
		logf("msg %q", f)
	}
	switch f[0] {
	case "r": // UI ready: push environment and window state
		ver, verErr := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
		if verErr != nil {
			ver = "unavailable: " + verErr.Error()
		}
		dpi := dpiFor(hwnd)
		post(
			"t"+fs+"env-runtime"+fs+ver,
			"t"+fs+"env-ncr"+fs+envInfo.ncr,
			"t"+fs+"env-dpi"+fs+fmt.Sprintf("%d (%.0f%%)", dpi, float64(dpi)/96*100),
			windowState(),
		)
		pushTabs()
		quickState.key = "" // the UI is new: draw the bar even if it looks unchanged
		refreshActions(true)
		if hub != nil {
			refreshProviders() // the agent panel and start page need to know what is ready
		}
		pushToolbar()
	case "n": // address bar submit
		if len(f) > 1 {
			navigateActive(f[1])
		}
	case "l": // page slot rectangle (css px)
		setSlot(f[1:])
	case "v": // provider management
		handleProviderOp(f)
	case "g": // chat with the agent
		handleChatOp(f)
	case "m": // menu popover state
		if len(f) > 1 {
			switch f[1] {
			case "open", "close":
				setMenuOpen(f[1] == "open")
			case "frozen":
				frozen()
			}
		}
	case "p": // ping → pong
		if len(f) > 1 {
			post("o" + fs + f[1])
		}
	case "i": // input (type-ahead probe)
		if len(f) > 3 {
			post("t" + fs + "typeahead" + fs + fmt.Sprintf("seq %s, %d chars", f[3], len([]rune(f[2]))))
		}
	case "c": // click / action
		if len(f) > 1 {
			id := ""
			if len(f) > 2 {
				id = f[2]
			}
			handleAction(f[1], id)
		}
	}
}

func handleAction(action, id string) {
	logf("action %s %q", action, id)
	switch action {
	case "tab.new":
		runLater(newTab)
	case "omnibox.focus":
		runLater(focusOmnibox)
	case "agent.stop":
		stopChat()
	case "config.open":
		openConfigFolder()
	case "menu.settings":
		runLater(openSettings)
	case "tab.select", "tab.close":
		n, err := strconv.Atoi(id)
		if err != nil {
			return
		}
		if action == "tab.select" {
			activate(n)
		} else {
			closeTab(n)
		}
	case "nav.back", "nav.forward", "nav.reload":
		navAction(action)
	case "win.min":
		pShowWindow.Call(hwnd, swMinimize)
	case "win.max":
		if isZoomed(hwnd) {
			pShowWindow.Call(hwnd, swRestore)
		} else {
			pShowWindow.Call(hwnd, swMaximize)
		}
	case "win.close":
		pPostMessageW.Call(hwnd, wmClose, 0, 0)
	}
}

func windowState() string {
	if isZoomed(hwnd) {
		return "s" + fs + "max"
	}
	return "s" + fs + "normal"
}

// post sends one or more records to the UI in a single web message.
func post(records ...string) {
	if core == nil {
		return
	}
	if err := core.PostWebMessageAsString(strings.Join(records, rs)); err != nil {
		fmt.Fprintln(os.Stderr, "post:", err)
	}
}

var uiView uintptr // the UI webview's child window

// lowerUI keeps the UI webview beneath every page webview.
func lowerUI() {
	if uiView != 0 {
		pSetWindowPos.Call(uiView, hwndBottom, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
}

// updatePadding insets the WebView so the parent window receives hit-tests at its
// edges for resizing. No inset when maximized.
func updatePadding() {
	if chromium == nil {
		return
	}
	b := paddingPx()
	chromium.SetPadding(edge.Rect{Left: b, Top: b, Right: b, Bottom: b})
}

func wndProc(h uintptr, m uint32, wParam, lParam uintptr) uintptr {
	switch m {
	case wmNCCalcSize:
		logf("WM_NCCALCSIZE wParam=%d zoomed=%v", wParam, isZoomed(h))
		if wParam != 0 {
			// The whole window rect is client area. When maximized the window overhangs
			// the monitor by the frame thickness, so pull it back in.
			if isZoomed(h) {
				p := (*ncCalcSizeParams)(unsafe.Pointer(lParam))
				dpi := dpiFor(h)
				fx := metric(smCxFrame, dpi) + metric(smCxPaddedBorder, dpi)
				fy := metric(smCyFrame, dpi) + metric(smCxPaddedBorder, dpi)
				p.rgrc[0].left += fx
				p.rgrc[0].right -= fx
				p.rgrc[0].top += fy
				p.rgrc[0].bottom -= fy
			}
			return 0
		}
	case wmNCHitTest:
		if isZoomed(h) {
			return htClient
		}
		var wr rect
		pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&wr)))
		x, y := loword(lParam), hiword(lParam)
		b := int32(6 * float64(dpiFor(h)) / 96)
		left, right := x < wr.left+b, x >= wr.right-b
		top, bottom := y < wr.top+b, y >= wr.bottom-b
		switch {
		case top && left:
			return htTopLeft
		case top && right:
			return htTopRight
		case bottom && left:
			return htBottomLeft
		case bottom && right:
			return htBottomRight
		case left:
			return htLeft
		case right:
			return htRight
		case top:
			return htTop
		case bottom:
			return htBottom
		}
		return htClient
	case wmGetMinMaxInfo:
		mmi := (*minMaxInfo)(unsafe.Pointer(lParam))
		s := float64(dpiFor(h)) / 96
		mmi.ptMinTrackSize = point{int32(900 * s), int32(560 * s)}
		return 0
	case wmSize:
		updatePadding()
		if chromium != nil {
			chromium.Resize()
		}
		lowerUI()
		layoutAll()
		if ready {
			post(windowState())
		}
		return 0
	case wmRunPending:
		runDeferred()
		return 0
	case wmDPIChanged:
		r := (*rect)(unsafe.Pointer(lParam))
		pSetWindowPos.Call(h, 0, uintptr(r.left), uintptr(r.top), uintptr(r.right-r.left), uintptr(r.bottom-r.top), swpNoZOrder|swpNoActivate)
		return 0
	case wmSetFocus:
		// Keyboard focus belongs to the page when a web tab is showing, else to the UI.
		if t := activeTab(); t != nil && t.cv != nil && !pageHidden {
			t.cv.Focus()
		} else if chromium != nil {
			chromium.Focus()
		}
		return 0
	case wmDestroy:
		if chromium != nil {
			chromium.ShuttingDown()
		}
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(h, uintptr(m), wParam, lParam)
	return r
}
