package main

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"phoenix/internal/config"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Tabs. Each web tab owns a WebView2 controller (a child HWND of the main window) that
// the host positions over the UI's #page-slot box. The UI webview only draws the tab
// strip; Go owns the list, the selection, and every page.

const (
	kindHome     = "home"
	kindWeb      = "web"
	kindSettings = "settings" // UI content like home: no page window
	kindFiles    = "files"    // the Files view: UI content too

	captureJPEG = 1 // COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_JPEG
)

type tab struct {
	id      int
	kind    string
	title   string
	url     string
	loading bool
	canBack bool
	canFwd  bool

	cv *edge.Chromium // nil for the home tab
	wv unsafe.Pointer // ICoreWebView2*

	browserEpoch atomic.Uint64 // bumped for each top-level navigation; invalidates page refs
}

var (
	tabs     []*tab
	activeID int
	nextID   = 1

	// Page-slot rectangle in CSS pixels, relative to the UI webview origin.
	slot     struct{ x, y, w, h float64 }
	haveSlot bool
	menuOpen bool // an overlay (the ⋯ menu) is open
	// pageHidden is true while the active page window is hidden behind a frozen image.
	pageHidden bool
	freezeGen  int

	browserDataPath string
	browserArgs     []string
	debugBuild      bool

	deferred   []func()
	deferredMu sync.Mutex
)

// runLater queues fn to run on the UI thread after the current callback has returned.
// Creating a controller pumps messages, so it must never happen inside a WebView2 callback.
// It is safe to call from any goroutine, and queued functions run in the order queued.
//
// Wails runs a function inline when it is already on the UI thread, so the hop through a
// goroutine is deliberate: it makes Wails post a message instead.
func runLater(fn func()) {
	deferredMu.Lock()
	deferred = append(deferred, fn)
	deferredMu.Unlock()
	go application.InvokeAsync(runDeferred)
}

func runDeferred() {
	deferredMu.Lock()
	q := deferred
	deferred = nil
	deferredMu.Unlock()
	for _, fn := range q {
		fn()
	}
}

func (t *tab) label() string {
	switch {
	case t.title != "":
		return t.title
	case t.kind == kindHome:
		return "Home"
	case t.url != "" && t.url != "about:blank":
		return t.url
	}
	return "New tab"
}

func findTab(id int) (int, *tab) {
	for i, t := range tabs {
		if t.id == id {
			return i, t
		}
	}
	return -1, nil
}

func activeTab() *tab {
	_, t := findTab(activeID)
	return t
}

func initTabs() {
	home := &tab{id: nextID, kind: kindHome, title: "Home"}
	nextID++
	tabs = []*tab{home}
	activeID = home.id
}

// ── Layout ─────────────────────────────────────────────────────────────────

// slotBounds converts the UI's CSS-pixel slot rectangle to physical pixels in the main
// window's client area.
func slotBounds() edge.Rect {
	s := float64(dpiFor(hwnd)) / 96
	l := int32(math.Round(slot.x * s))
	t := int32(math.Round(slot.y * s))
	return edge.Rect{
		Left:   l,
		Top:    t,
		Right:  l + int32(math.Round(slot.w*s)),
		Bottom: t + int32(math.Round(slot.h*s)),
	}
}

// place shows the tab's page over the slot if it is the active tab, hides it otherwise.
func (t *tab) place() {
	if t.cv == nil {
		return
	}
	ctrl := t.cv.GetController()
	if t.id == activeID && haveSlot && !pageHidden {
		_ = ctrl.PutBounds(slotBounds())
		_ = ctrl.PutIsVisible(true)
	} else {
		_ = ctrl.PutIsVisible(false)
	}
}

func layoutAll() {
	lowerUI()
	for _, t := range tabs {
		t.place()
	}
}

// ── State pushed to the UI ─────────────────────────────────────────────────

type tabJSON struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Kind    string `json:"kind"`
	Active  bool   `json:"active"`
	Loading bool   `json:"loading"`
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func pushTabs() {
	list := make([]tabJSON, len(tabs))
	for i, t := range tabs {
		list[i] = tabJSON{t.id, t.label(), t.url, t.kind, t.id == activeID, t.loading}
	}
	post("u" + fs + "tabs" + fs + "tabs" + fs + jsonString(list))
}

func pushToolbar() {
	t := activeTab()
	if t == nil {
		return
	}
	view, addr := t.kind, "" // the view names match the tab kinds
	if t.kind == kindWeb && t.url != "about:blank" {
		addr = t.url
	}
	post(
		"a"+fs+"app"+fs+"data-view"+fs+view,
		"u"+fs+"omnibox"+fs+"value"+fs+jsonString(addr),
		"u"+fs+"nav-back"+fs+"disabled"+fs+strconv.FormatBool(!t.canBack),
		"u"+fs+"nav-forward"+fs+"disabled"+fs+strconv.FormatBool(!t.canFwd),
	)
}

// ── Operations ─────────────────────────────────────────────────────────────

func activate(id int) {
	if _, t := findTab(id); t == nil {
		return
	}
	activeID = id
	layoutAll()
	if t := activeTab(); t != nil && t.cv != nil && !menuOpen {
		t.cv.Focus()
	}
	pushTabs()
	pushToolbar()
	refreshActions(true)
}

func createWebTab(rawURL string) {
	logf("createWebTab %q", rawURL)
	t := &tab{id: nextID, kind: kindWeb, url: rawURL, loading: true}
	nextID++
	if !prepareWebTab(t) {
		return
	}
	tabs = append(tabs, t)
	activate(t.id)
	t.cv.Navigate(rawURL)
}

// prepareWebTab attaches a page WebView and its host-side handlers to an existing tab.
// It lets a home tab become a browsing tab without leaving an extra blank tab behind.
func prepareWebTab(t *tab) bool {
	t.browserEpoch.Store(1)
	cv := edge.NewChromium()
	cv.DataPath = browserDataPath
	cv.AdditionalBrowserArgs = browserArgs
	cv.SetErrorCallback(func(err error) { logf("tab webview2: %v\n%s", err, debug.Stack()) })
	t.cv = cv

	// Must never fire: web messaging is disabled for pages. Logged so a regression is visible.
	cv.MessageCallback = func(m string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		logf("SECURITY: tab %d delivered a web message (%d bytes)", t.id, len(m))
	}
	cv.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
		t.loading = false
		if comGetBool(unsafe.Pointer(args), navCompletedGetIsSuccess) {
			if current := comGetString(t.wv, wvGetSource); current != "" {
				t.url = current
			}
			recordVisited(t.url, t.title)
		}
		pushTabs()
		if t.id == activeID {
			runLater(func() { refreshActions(false) })
			scheduleActionRefresh(t.id, 1500*time.Millisecond) // pages that build themselves late
		}
	}

	if !cv.Embed(hwnd) {
		logf("tab %d: embed failed", t.id)
		t.cv = nil
		return false
	}
	ctrl := cv.GetController()
	core, err := ctrl.GetCoreWebView2()
	if err != nil {
		logf("tab %d: GetCoreWebView2: %v", t.id, err)
		comCall(unsafe.Pointer(cv.GetController()), ctrlClose)
		t.cv = nil
		return false
	}
	t.wv = unsafe.Pointer(core)

	// Page content is untrusted: it must not be able to message Go.
	if s, err := cv.GetSettings(); err == nil {
		perr := s.PutIsWebMessageEnabled(false)
		got, gerr := s.GetIsWebMessageEnabled()
		logf("tab %d: web message put err=%v, readback=%v err=%v", t.id, perr, got, gerr)
		_ = s.PutAreHostObjectsAllowed(false)
		// Right-click → Inspect and F12. Pages are the user's own browsing, not Phoenix's UI.
		_ = s.PutAreDevToolsEnabled(true)
		_ = s.PutAreDefaultContextMenusEnabled(true)
		_ = s.PutIsZoomControlEnabled(true)
	}
	attachShortcuts(ctrl)

	must := func(slot uintptr, what string, fn func(sender, args unsafe.Pointer)) {
		if err := addEvent(t.wv, slot, what, fn); err != nil {
			logf("tab %d: %v", t.id, err)
		}
	}
	must(wvAddNavigationStarting, "NavigationStarting", func(_, args unsafe.Pointer) {
		t.browserEpoch.Add(1)
		t.loading = true
		t.title = ""
		if u := comGetString(args, navStartingGetURI); u != "" {
			t.url = u
		}
		pushTabs()
		if t.id == activeID {
			pushToolbar()
			refreshActions(true)
		}
	})
	must(wvAddSourceChanged, "SourceChanged", func(_, _ unsafe.Pointer) {
		if u := comGetString(t.wv, wvGetSource); u != "" {
			t.url = u
		}
		if t.id == activeID {
			pushToolbar()
		}
	})
	must(wvAddHistoryChanged, "HistoryChanged", func(_, _ unsafe.Pointer) {
		t.canBack = comGetBool(t.wv, wvGetCanGoBack)
		t.canFwd = comGetBool(t.wv, wvGetCanGoForward)
		if t.id == activeID {
			pushToolbar()
		}
	})
	must(wvAddDocumentTitleChanged, "DocumentTitleChanged", func(_, _ unsafe.Pointer) {
		t.title = comGetString(t.wv, wvGetDocumentTitle)
		updateVisitedTitle(t.url, t.title)
		pushTabs()
	})
	must(wvAddNewWindowRequested, "NewWindowRequested", func(_, args unsafe.Pointer) {
		uri := comGetString(args, newWindowGetURI)
		comCall(args, newWindowPutHandled, 1) // we open it ourselves, as a tab
		if uri != "" && uri != "about:blank" && !allowedURL(uri) {
			return
		}
		// A page-opened window (a "Sign in with Google" popup, a payment window) must stay
		// connected to the page that opened it: the opener receives the result through
		// window.opener / postMessage and the popup closes itself. Giving WebView2 the new
		// tab's own webview as the target keeps that link; a plain new tab would lose it and
		// the sign-in would end on a blank success page.
		var deferral unsafe.Pointer
		if hr := comCall(args, newWindowGetDeferral, uintptr(unsafe.Pointer(&deferral))); hr != 0 || deferral == nil {
			if allowedURL(uri) {
				runLater(func() { createWebTab(uri) })
			}
			return
		}
		runLater(func() {
			defer comCall(deferral, deferralComplete)
			child := &tab{id: nextID, kind: kindWeb, url: uri, loading: true}
			nextID++
			if !prepareWebTab(child) {
				return
			}
			comCall(args, newWindowPutNewWindow, uintptr(child.wv))
			tabs = append(tabs, child)
			activate(child.id)
		})
	})
	must(wvAddWindowCloseRequested, "WindowCloseRequested", func(_, _ unsafe.Pointer) {
		runLater(func() { closeTab(t.id) }) // window.close() from the page
	})

	return true
}

func closeTab(id int) {
	i, t := findTab(id)
	if t == nil {
		return
	}
	forgetBrowserTab(id)
	if t.cv != nil {
		comCall(unsafe.Pointer(t.cv.GetController()), ctrlClose)
	}
	tabs = append(tabs[:i], tabs[i+1:]...)
	if len(tabs) == 0 {
		home := &tab{id: nextID, kind: kindHome, title: "Home"}
		nextID++
		tabs = append(tabs, home)
	}
	if activeID == id {
		j := min(i, len(tabs)-1)
		if i > 0 {
			j = i - 1
		}
		activeID = tabs[j].id
	}
	activate(activeID)
}

func navigateActive(input string) {
	target := normalizeInput(input)
	if target == "" {
		return
	}
	t := activeTab()
	if t == nil || t.kind == kindSettings || t.kind == kindFiles {
		runLater(func() { createWebTab(target) })
		return
	}
	if t.kind == kindHome {
		t.kind, t.title, t.url, t.loading = kindWeb, "", target, true
		if !prepareWebTab(t) {
			t.kind, t.title, t.url, t.loading = kindHome, "Home", "", false
			pushTabs()
			pushToolbar()
			return
		}
		layoutAll()
		pushTabs()
		pushToolbar()
		refreshActions(true)
		t.cv.Navigate(target)
		return
	}
	t.cv.Navigate(target)
}

func navAction(action string) {
	t := activeTab()
	if t == nil || t.kind != kindWeb {
		return
	}
	switch action {
	case "nav.back":
		comCall(t.wv, wvGoBack)
	case "nav.forward":
		comCall(t.wv, wvGoForward)
	case "nav.reload":
		comCall(t.wv, wvReload)
	}
}

func setSlot(f []string) {
	if len(f) < 4 {
		return
	}
	v := [4]float64{}
	for i := range v {
		x, err := strconv.ParseFloat(f[i], 64)
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) || x < 0 || x > 20000 {
			return
		}
		v[i] = x
	}
	slot.x, slot.y, slot.w, slot.h = v[0], v[1], v[2], v[3]
	haveSlot = slot.w > 0 && slot.h > 0
	layoutAll()
}

// The airspace problem: a page's WebView2 is its own child window, so it always draws
// over the UI webview, including over popovers that extend into the slot. While an
// overlay is open we capture the page, show the image in the UI's #freeze element, and
// only then hide the page window, so the user sees no change underneath the overlay.

func setMenuOpen(open bool) {
	if menuOpen == open {
		return
	}
	menuOpen = open
	freezeGen++
	if open {
		freezeActive(freezeGen)
		return
	}
	pageHidden = false
	layoutAll()
	if t := activeTab(); t != nil && t.cv != nil {
		t.cv.Focus()
	}
	post("u" + fs + "freeze" + fs + "hidden" + fs + "true")
}

// frozen is the UI's acknowledgement that the frozen image has been painted.
func frozen() {
	if !menuOpen {
		return
	}
	pageHidden = true
	layoutAll()
}

func freezeActive(gen int) {
	t := activeTab()
	if t == nil || t.cv == nil {
		return // the home view is UI content; nothing overlaps it
	}
	start := time.Now()
	stream := newMemStream()
	if stream == nil {
		pageHidden = true
		layoutAll()
		return
	}
	h := newHandler(func(sender, _ unsafe.Pointer) {
		defer comCall(stream, 2) // Release
		if gen != freezeGen || !menuOpen {
			return // the overlay closed before the capture finished
		}
		if hr := uintptr(sender); int32(hr) < 0 {
			logf("capture failed hr=0x%08x", uint32(hr))
			frozen()
			return
		}
		data, err := readAll(stream)
		if err != nil {
			logf("capture read: %v", err)
			frozen()
			return
		}
		logf("capture: %d bytes in %v", len(data), time.Since(start))
		post("u"+fs+"freeze"+fs+"src"+fs+jsonString("data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(data)),
			"u"+fs+"freeze"+fs+"hidden"+fs+"false")
	})
	hr := comCall(t.wv, wvCapturePreview, captureJPEG, uintptr(stream), uintptr(unsafe.Pointer(h)))
	if err := hrErr("CapturePreview", hr); err != nil {
		logf("%v", err)
		comCall(stream, 2)
		pageHidden = true
		layoutAll()
	}
}

// ── Address handling ───────────────────────────────────────────────────────

func allowedURL(u string) bool {
	if u == "about:blank" {
		return true
	}
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https")
}

// normalizeInput turns what the user typed into a URL: an address if it looks like one,
// a search otherwise. Returns "" for input that must not be navigated to.
func normalizeInput(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(s, "://"):
		if allowedURL(s) {
			return s
		}
		return "" // file:, javascript:, custom schemes are not navigated from the address bar
	case lower == "about:blank":
		return s
	case strings.ContainsAny(s, " \t") || !strings.Contains(s, ".") && !strings.HasPrefix(lower, "localhost"):
		return searchURL(s)
	case strings.HasPrefix(lower, "localhost") || strings.HasPrefix(lower, "127.0.0.1"):
		return "http://" + s
	}
	return "https://" + s
}

// searchURL builds the search address from the configured template (https only, with one
// {query}; config.ValidSearchURL has already checked it).
func searchURL(q string) string {
	tmpl := config.DefaultSearchURL
	if cfgStore != nil {
		cfgStore.View(func(c *config.Config) { tmpl = c.SearchURL() })
	}
	return strings.Replace(tmpl, "{query}", url.QueryEscape(q), 1)
}
