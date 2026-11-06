// Command phoenix is the Phoenix desktop workspace: a Wails v3 application whose UI webview
// is Wails's own and whose browser tabs are WebView2 controllers hosted as child windows of
// the Wails window (Wails v3 cannot host several webviews in one window yet).
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/go-webview2/webviewloader"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"phoenix/internal/ui"
)

const (
	fs = "\u001f" // field separator
	rs = "\u001e" // record separator
)

//go:embed tray.ico
var trayIcon []byte

var (
	app      *application.App
	win      *application.WebviewWindow
	hwnd     uintptr   // the Wails window's HWND, known once the UI has loaded
	uiViews  []uintptr // Wails's own webview windows: they must stay beneath the tab pages
	ready    bool      // the UI has loaded and can receive messages
	started  bool      // tabs and providers are initialised
	quitting bool
	restarting bool
	logFile  *os.File
)

// logf writes diagnostics to %TEMP%\phoenix.log when PHOENIX_LOG=1.
func logf(format string, args ...any) {
	if logFile == nil {
		return
	}
	fmt.Fprintf(logFile, format+"\n", args...)
}

func main() {
	// The agent's CLI starts this program as its desktop tool server; no window, no Wails.
	if len(os.Args) > 1 && os.Args[1] == "mcp-desktop" {
		os.Exit(runDesktopMCP())
	}
	if len(os.Args) > 1 && os.Args[1] == "mcp-files" {
		os.Exit(runFilesMCP())
	}
	if len(os.Args) > 1 && os.Args[1] == "mcp-browser" {
		os.Exit(runBrowserMCP())
	}
	if os.Getenv("PHOENIX_LOG") == "1" {
		logFile, _ = os.Create(filepath.Join(os.TempDir(), "phoenix.log"))
	}
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		fmt.Fprintln(os.Stderr, "phoenix: LOCALAPPDATA is not set")
		os.Exit(1)
	}
	// Pages keep the profile earlier builds used, so sign-ins survive the move to Wails.
	browserDataPath = filepath.Join(local, "Phoenix", "spike", "browser")
	debugBuild = os.Getenv("PHOENIX_DEBUG") == "1"

	// Dev only: a DevTools port on loopback for scripts. The UI and the pages are separate
	// browser processes, so the pages use the next port. Never enable in a release build.
	// The agent keeps working while Phoenix is minimized, covered by other windows or hidden in
	// the tray. Chromium would otherwise treat those pages as hidden and throttle their timers,
	// pause rendering and freeze background tabs, so scripts, lazy loading and screenshots stall.
	browserArgs = []string{
		"--disable-features=CalculateNativeWinOcclusion",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-background-timer-throttling",
	}
	var uiArgs []string
	if port, err := strconv.Atoi(os.Getenv("PHOENIX_DEBUG_PORT")); err == nil && port > 0 {
		uiArgs = []string{"--remote-debugging-port=" + strconv.Itoa(port), "--remote-debugging-address=127.0.0.1"}
		browserArgs = append(browserArgs, "--remote-debugging-port="+strconv.Itoa(port+1), "--remote-debugging-address=127.0.0.1")
	}

	app = application.New(application.Options{
		Name:        "Phoenix",
		Description: "A desktop workspace where you and an AI agent browse and work together",
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(ui.Assets())},
		// The UI talks to Go with plain strings (see ui/src/protocol.ts), not bound methods.
		RawMessageHandler: func(_ application.Window, message string, _ *application.OriginInfo) {
			runLater(func() { onMessage(message) })
		},
		Windows: application.WindowsOptions{
			AdditionalBrowserArgs: uiArgs,
			WebviewUserDataPath:   filepath.Join(local, "Phoenix", "ui"),
		},
		SingleInstance: &application.SingleInstanceOptions{
			// An isolated test profile must not hand over to (or be blocked by) the real app.
			UniqueID: singleInstanceID(),
			// Starting Phoenix again brings the running one forward.
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { runLater(showWindow) },
		},
	})

	win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Phoenix",
		Width:            1280,
		Height:           800,
		MinWidth:         900,
		MinHeight:        560,
		Frameless:        true,
		URL:              "/",
		BackgroundColour: application.NewRGB(0x15, 0x0e, 0x12),
		// The UI is not a web page: no browser menus, devtools or zoom unless debugging.
		DevToolsEnabled:            debugBuild,
		DefaultContextMenuDisabled: !debugBuild,
		ZoomControlEnabled:         false,
		Windows: application.WindowsWindow{
			// Lets the UI's CSS `app-region: drag` title bar move the window.
			NonClientRegionSupport: true,
			Theme:                  application.Dark,
		},
		KeyBindings: uiKeyBindings(),
	})

	// Closing the window keeps Phoenix running in the tray; "Quit" in the tray really exits.
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !quitting {
			e.Cancel()
			win.Hide()
		}
	})
	win.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) { runLater(onResized) })
	win.OnWindowEvent(events.Windows.WindowMaximise, func(*application.WindowEvent) { runLater(pushWindowState) })
	win.OnWindowEvent(events.Windows.WindowUnMaximise, func(*application.WindowEvent) { runLater(pushWindowState) })
	win.OnWindowEvent(events.Windows.WindowRestore, func(*application.WindowEvent) { runLater(pushWindowState) })
	win.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) { runLater(focusContent) })

	setupTray()

	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "phoenix:", err)
		os.Exit(1)
	}
	if restarting {
		if exe, err := os.Executable(); err == nil {
			if err := exec.Command(exe).Start(); err != nil {
				fmt.Fprintln(os.Stderr, "phoenix restart:", err)
			}
		}
	}
}

func setupTray() {
	menu := app.NewMenu()
	menu.Add("Show Phoenix").OnClick(func(*application.Context) { runLater(showWindow) })
	menu.AddSeparator()
	menu.Add("Quit Phoenix").OnClick(func(*application.Context) {
		quitting = true
		flushAgentDataWrites()
		if hub != nil {
			hub.Close() // stops the MCP servers Phoenix started
			cleanupUploads()
		}
		app.Quit()
	})
	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("Phoenix")
	tray.SetMenu(menu)
	tray.OnClick(func() { runLater(showWindow) })
}

func showWindow() {
	win.Show()
	if win.IsMinimised() {
		win.UnMinimise()
	}
	win.Focus()
}

// ── Messages from the UI ───────────────────────────────────────────────────

// onMessage handles one UI to Go message. It runs on the UI thread, in the order sent.
func onMessage(message string) {
	f := strings.Split(message, fs)
	switch f[0] {
	case "p", "i":
	case "g":
		// Chat text is private: log the operation only.
		if len(f) > 1 {
			logf("msg g %s", f[1])
		}
	case "f":
		// File uploads carry file contents: log the operation only.
		if len(f) > 1 {
			logf("msg f %s", f[1])
		}
	case "v":
		// Provider messages can carry an API key: log the operation, never the fields. For
		// MCP the second field can be pasted JSON holding secrets, so only the operation is
		// logged.
		switch {
		case len(f) < 2:
		case strings.HasPrefix(f[1], "mcp-"):
			logf("msg v %s", f[1])
		case len(f) > 2:
			logf("msg v %s %s", f[1], f[2])
		}
	default:
		logf("msg %q", f)
	}
	switch f[0] {
	case "r": // UI ready: start up on the first one, then push state
		uiReady()
	case "n": // address bar submit
		if len(f) > 1 {
			navigateActive(f[1])
		}
	case "l": // page slot rectangle (css px)
		setSlot(f[1:])
	case "v": // provider management
		handleProviderOp(f)
	case "f": // the Files view
		handleFilesOp(f)
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
	case "p": // ping, answered with a pong
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

func uiReady() {
	hwnd = uintptr(win.NativeWindow())
	restoreChat := !started
	if !started {
		started = true
		// Everything in the Wails window so far is Wails's own UI webview. Tab pages are
		// added later and must draw over it.
		uiViews = childWindows()
		lowerUI()
		initTabs()
		initHistory()
		initAgentPersistence()
		initProviders()
		startDevScript()
		startAppInit() // library, approvals, saved MCP servers, desktop thread: all in the background
	}
	ready = true
	ver, verErr := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if verErr != nil {
		ver = "unavailable: " + verErr.Error()
	}
	dpi := dpiFor(hwnd)
	post(
		"t"+fs+"env-runtime"+fs+ver,
		"t"+fs+"env-ncr"+fs+"Wails",
		"t"+fs+"env-dpi"+fs+fmt.Sprintf("%d (%.0f%%)", dpi, float64(dpi)/96*100),
		windowState(),
	)
	pushTabs()
	pushHistory()
	pushSavedChats()
	pushAgentActivity()
	if restoreChat {
		restoreLatestSavedChat()
	}
	quickState.key = "" // the UI is new: draw the bar even if it looks unchanged
	refreshActions(true)
	refreshProviders() // also reports a configuration error instead of leaving Home in "checking"
	if hub != nil {
		pushMCP("", false) // slash commands need the installed server list before Settings opens
	}
	pushToolbar()
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
	case "history.open":
		openHistoryEntry(id)
	case "history.delete":
		deleteHistoryEntry(id)
	case "history.clear":
		clearHistory()
	case "app.restart":
		quitting, restarting = true, true
		flushAgentDataWrites()
		if hub != nil {
			hub.Close()
		}
		app.Quit()
	case "setup.get": // the setup card's "Get Claude Code" / "Get Codex"
		runLater(func() { openSetupPage(id) })
	case "menu.settings":
		runLater(openSettings)
	case "menu.files":
		runLater(openFiles)
	case "menu.browser":
		runLater(showBrowser)
	case "tab.select", "tab.close":
		n, err := strconv.Atoi(id)
		if err != nil {
			return
		}
		if action == "tab.select" {
			activate(n)
			_, t := findTab(n)
			adoptTab(t) // the user chose it, so it is theirs now
		} else {
			closeTab(n)
		}
	case "nav.back", "nav.forward", "nav.reload":
		navAction(action)
	case "win.min":
		win.Minimise()
	case "win.max":
		win.ToggleMaximise()
	case "win.close":
		win.Close()
	}
}

func windowState() string {
	if isZoomed(hwnd) {
		return "s" + fs + "max"
	}
	return "s" + fs + "normal"
}

func pushWindowState() {
	if ready {
		post(windowState())
	}
}

// ── Messages to the UI ─────────────────────────────────────────────────────

// post sends one or more records to the UI in a single message. The UI's `__phx` function
// (ui/src/protocol.ts) splits them. The payload is a JSON string literal, so nothing in it
// can break out of the call.
func post(records ...string) {
	if !ready || win == nil {
		return
	}
	payload, err := json.Marshal(strings.Join(records, rs))
	if err != nil {
		return
	}
	win.ExecJS("window.__phx&&window.__phx(" + string(payload) + ")")
}

// ── Window layout ──────────────────────────────────────────────────────────

// lowerUI keeps Wails's webview beneath every page webview.
func lowerUI() {
	for _, h := range uiViews {
		pSetWindowPos.Call(h, hwndBottom, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
}

// onResized re-lays out the tab pages after the window changed size. Resizing the UI webview
// raises its window, so the order is re-asserted.
func onResized() {
	if !started {
		return
	}
	lowerUI()
	layoutAll()
	pushWindowState()
}

// focusContent gives keyboard focus to the page when a web tab is showing.
func focusContent() {
	if t := activeTab(); t != nil && t.cv != nil && !pageHidden {
		t.cv.Focus()
	}
}

// uiKeyBindings are the browser shortcuts while the UI (not a page) has the keyboard. The
// same actions are attached to each page controller in keys_windows.go.
func uiKeyBindings() map[string]func(application.Window) {
	run := func(fn func()) func(application.Window) {
		return func(application.Window) { runLater(fn) }
	}
	m := map[string]func(application.Window){
		"alt+left":       run(func() { navAction("nav.back") }),
		"alt+right":      run(func() { navAction("nav.forward") }),
		"alt+d":          run(focusOmnibox),
		"f6":             run(focusOmnibox),
		"ctrl+l":         run(focusOmnibox),
		"f5":             run(func() { navAction("nav.reload") }),
		"ctrl+r":         run(func() { navAction("nav.reload") }),
		"f12":            run(openDevTools),
		"ctrl+t":         run(newTab),
		"ctrl+,":         run(openSettings),
		"ctrl+w":         run(func() { closeTab(activeID) }),
		"ctrl+f4":        run(func() { closeTab(activeID) }),
		"ctrl+tab":       run(func() { cycleTab(+1) }),
		"ctrl+shift+tab": run(func() { cycleTab(-1) }),
		"ctrl+page down": run(func() { cycleTab(+1) }),
		"ctrl+page up":   run(func() { cycleTab(-1) }),
	}
	for i := 1; i <= 9; i++ {
		n, last := i-1, i == 9
		m["ctrl+"+strconv.Itoa(i)] = run(func() { selectNth(n, last) })
	}
	return m
}

func singleInstanceID() string {
	if os.Getenv("PHOENIX_TEST_PROFILE") != "" {
		return "com.phoenix.desktop.test"
	}
	return "com.phoenix.desktop"
}
