// Spike: can a Wails v3 application (shell, UI webview, raw messages) host Phoenix's own
// WebView2 tab controllers as child windows of the Wails window? Windows only.
package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets
var assets embed.FS

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	pGetDpiForWnd   = user32.NewProc("GetDpiForWindow")
	pGetClientRect  = user32.NewProc("GetClientRect")
	pEnumChildWnds  = user32.NewProc("EnumChildWindows")
	pGetClassNameW  = user32.NewProc("GetClassNameW")
	pSetWindowPos   = user32.NewProc("SetWindowPos")
	pGetWindow      = user32.NewProc("GetWindow")
	pGetWindowTextW = user32.NewProc("GetWindowTextW")
)

type rect struct{ L, T, R, B int32 }

var (
	win      *application.WebviewWindow
	mu       sync.Mutex
	tab      *edge.Chromium
	slotCSS  [4]float64
	haveSlot bool
	logf     *os.File
)

func say(format string, a ...any) {
	line := fmt.Sprintf(format, a...)
	fmt.Println(line)
	if logf != nil {
		fmt.Fprintln(logf, line)
	}
	if win != nil {
		win.ExecJS(fmt.Sprintf("window.__phx && window.__phx(%q)", line))
	}
}

func hwnd() uintptr { return uintptr(win.NativeWindow()) }

func dpiScale() float64 {
	d, _, _ := pGetDpiForWnd.Call(hwnd())
	if d == 0 {
		d = 96
	}
	return float64(d) / 96
}

// children lists the child windows of the Wails window, bottom-most last (EnumChildWindows
// walks top to bottom).
func children() []string {
	var out []string
	cb := syscall.NewCallback(func(h uintptr, _ uintptr) uintptr {
		cls := make([]uint16, 128)
		pGetClassNameW.Call(h, uintptr(unsafe.Pointer(&cls[0])), 128)
		var r rect
		pGetClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
		out = append(out, fmt.Sprintf("%#x %s %dx%d", h, syscall.UTF16ToString(cls), r.R-r.L, r.B-r.T))
		return 1
	})
	pEnumChildWnds.Call(hwnd(), cb, 0)
	return out
}

// directChildren lists the immediate child windows of the Wails window, top first.
func directChildren() []uintptr {
	var out []uintptr
	h, _, _ := pGetWindow.Call(hwnd(), 5) // GW_CHILD
	for h != 0 {
		out = append(out, h)
		h, _, _ = pGetWindow.Call(h, 2) // GW_HWNDNEXT
	}
	return out
}

// uiViews are the Wails UI webview windows: every direct child that is not a tab.
var uiViews []uintptr

// lowerUI puts Wails's own webview under the tab pages (the airspace rule: a child window
// always paints over its siblings that are lower in z-order).
func lowerUI() {
	for _, h := range uiViews {
		pSetWindowPos.Call(h, 1, 0, 0, 0, 0, 0x13) // HWND_BOTTOM, NOMOVE|NOSIZE|NOACTIVATE
	}
}

func place() {
	if tab == nil || !haveSlot {
		return
	}
	lowerUI()
	s := dpiScale()
	ctrl := tab.GetController()
	_ = ctrl.PutBounds(edge.Rect{
		Left:   int32(slotCSS[0] * s),
		Top:    int32(slotCSS[1] * s),
		Right:  int32((slotCSS[0] + slotCSS[2]) * s),
		Bottom: int32((slotCSS[1] + slotCSS[3]) * s),
	})
}

func openTab(url string) {
	if uiViews == nil {
		uiViews = directChildren() // everything there is before the first tab is the Wails UI
	}
	cv := edge.NewChromium()
	cv.DataPath = filepath.Join(os.Getenv("LOCALAPPDATA"), "PhoenixHybridSpike", "browser")
	cv.SetErrorCallback(func(err error) { say("tab error: %v", err) })
	if !cv.Embed(hwnd()) {
		say("tab embed FAILED")
		return
	}
	tab = cv
	place()
	lowerUI()
	_ = cv.GetController().PutIsVisible(true)
	cv.Navigate(url)
	say("tab embedded; children (top first): %s", strings.Join(children(), " | "))
}

func onRaw(_ application.Window, msg string, _ *application.OriginInfo) {
	f := strings.Fields(msg)
	if len(f) == 0 {
		return
	}
	switch f[0] {
	case "ping":
		say("pong for %s", f[1])
	case "slot":
		var v [4]float64
		for i := 0; i < 4; i++ {
			v[i], _ = strconv.ParseFloat(f[1+i], 64)
		}
		application.InvokeSync(func() { slotCSS, haveSlot = v, true; place() })
	case "tab":
		application.InvokeSync(func() { openTab(f[1]) })
	case "vis":
		application.InvokeSync(func() {
			if tab != nil {
				_ = tab.GetController().PutIsVisible(f[1] == "1")
			}
		})
	}
}

func debugArgs() []string {
	if p := os.Getenv("SPIKE_DEBUG_PORT"); p != "" {
		return []string{"--remote-debugging-port=" + p}
	}
	return nil
}

func main() {
	if os.Getenv("SPIKE_LOG") != "" {
		logf, _ = os.Create(os.Getenv("SPIKE_LOG"))
	}
	app := application.New(application.Options{
		Name:              "Phoenix hybrid spike",
		Assets:            application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		RawMessageHandler: onRaw,
		Windows:           application.WindowsOptions{AdditionalBrowserArgs: debugArgs()},
	})
	win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Phoenix hybrid spike",
		Width:     1100,
		Height:    700,
		Frameless: true,
		URL:       "/",
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
