package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"phoenix/internal/browser"
	"phoenix/internal/desktop"
)

const (
	// Every tool result is replayed to the model on every later step of the run, so a smaller
	// snapshot is the biggest speed-up there is. A busy page is searched with query, not listed.
	maxSnapshotNodes = 150
	maxBrowserTabs   = 30
	maxBrowserText   = 16 << 10
)

type browserPermission struct {
	Class   desktop.Class
	App     string
	Summary string
}

type browserRef struct {
	BackendNodeID int
	NodeID        int
	Role          string
	Name          string
	Checked       bool
}

type browserSnapshot struct {
	epoch uint64
	nonce string
	refs  map[string]browserRef
}

type browserSession struct {
	opMu     sync.Mutex
	snapshot browserSnapshot
	extract  extractCache
}

var browserSessions = struct {
	sync.Mutex
	byTab map[int]*browserSession
}{byTab: make(map[int]*browserSession)}

type browserTabState struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Host    string `json:"host,omitempty"`
	Kind    string `json:"kind"`
	Active  bool   `json:"active"`
	Loading bool   `json:"loading"`
	CanBack bool   `json:"can_back"`
	CanFwd  bool   `json:"can_forward"`
	Epoch   uint64 `json:"-"`
}

func browserSessionFor(id int) *browserSession {
	browserSessions.Lock()
	s := browserSessions.byTab[id]
	if s == nil {
		s = &browserSession{}
		browserSessions.byTab[id] = s
	}
	browserSessions.Unlock()
	return s
}

// Called from closeTab on the UI thread. IDs are monotonic and never reused, so an
// operation already holding the old session can safely finish and observe the closed tab.
func forgetBrowserTab(id int) {
	browserSessions.Lock()
	delete(browserSessions.byTab, id)
	browserSessions.Unlock()
}

type browserUIResult struct {
	value any
	err   error
}

func browserOnUI(ctx context.Context, fn func() (any, error)) (any, error) {
	ch := make(chan browserUIResult, 1)
	runLater(func() {
		if err := ctx.Err(); err != nil {
			ch <- browserUIResult{err: err}
			return
		}
		value, err := fn()
		ch <- browserUIResult{value: value, err: err}
	})
	select {
	case r := <-ch:
		return r.value, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func browserTab(ctx context.Context, id int) (browserTabState, error) {
	value, err := browserOnUI(ctx, func() (any, error) {
		_, t := findTab(id)
		if t == nil {
			return nil, errors.New("the browser tab is no longer available")
		}
		state := browserTabState{
			ID: t.id, Title: clipBrowser(t.label(), 120), URL: safeTabURL(t.url), Kind: t.kind,
			Active: t.id == activeID, Loading: t.loading, CanBack: t.canBack, CanFwd: t.canFwd,
			Epoch: t.browserEpoch.Load(),
		}
		state.Host = browserHost(t.url)
		return state, nil
	})
	if err != nil {
		return browserTabState{}, err
	}
	return value.(browserTabState), nil
}

func browserTabs(ctx context.Context) ([]browserTabState, error) {
	value, err := browserOnUI(ctx, func() (any, error) {
		limit := min(len(tabs), maxBrowserTabs)
		out := make([]browserTabState, 0, limit)
		for _, t := range tabs[:limit] {
			state := browserTabState{
				ID: t.id, Title: clipBrowser(t.label(), 100), URL: safeTabURL(t.url), Kind: t.kind,
				Active: t.id == activeID, Loading: t.loading, CanBack: t.canBack, CanFwd: t.canFwd,
			}
			state.Host = browserHost(t.url)
			out = append(out, state)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]browserTabState), nil
}

func browserCallMeta(ctx context.Context, call browser.Call) (browserPermission, error) {
	args, err := decodeBrowserCall(call)
	if err != nil {
		return browserPermission{}, err
	}
	meta := browserPermission{Class: desktop.Act, App: "Phoenix browser"}
	if call.Tool == "browser_tabs" {
		meta.Class, meta.Summary = desktop.Look, "Read the list of open Phoenix browser tabs"
		return meta, nil
	}
	if call.Tool == "browser_open_tab" {
		a := args.(browser.OpenTabArgs)
		meta.Summary = "Open a new Phoenix browser tab"
		if strings.TrimSpace(a.URL) != "" {
			meta.Summary += " at " + safeTabURL(a.URL)
		}
		return meta, nil
	}
	if call.Tool == "browser_navigate" {
		a := args.(browser.NavigateArgs)
		t, e := browserTab(ctx, a.TabID)
		if e != nil {
			return browserPermission{}, e
		}
		meta.App = browserApp(t.Host)
		meta.Summary = fmt.Sprintf("Navigate tab %d to %s", a.TabID, safeTabURL(a.URL))
		return meta, nil
	}
	if call.Tool == "browser_back" || call.Tool == "browser_forward" || call.Tool == "browser_reload" || call.Tool == "browser_close_tab" || call.Tool == "browser_switch_tab" {
		a := args.(browser.TabArgs)
		t, e := browserTab(ctx, a.TabID)
		if e != nil {
			return browserPermission{}, e
		}
		meta.App = browserApp(t.Host)
		meta.Summary = map[string]string{
			"browser_back":       "Go back in the browser history",
			"browser_forward":    "Go forward in the browser history",
			"browser_reload":     "Reload the browser tab",
			"browser_close_tab":  "Close the browser tab",
			"browser_switch_tab": "Switch to the browser tab",
		}[call.Tool]
		meta.Summary = fmt.Sprintf("%s in tab %d at %s", meta.Summary, a.TabID, browserApp(t.Host))
		return meta, nil
	}
	var tabID int
	switch a := args.(type) {
	case browser.SnapshotArgs:
		tabID = a.TabID
		meta.Class = desktop.Look
		meta.Summary = "Read an accessible outline of the page"
	case browser.TabArgs:
		tabID = a.TabID
		switch call.Tool {
		case "browser_copy":
			meta.Summary = "Copy the page selection to the clipboard"
		case "browser_paste":
			meta.Summary = "Paste the clipboard into the focused page field"
		case "browser_read":
			meta.Class = desktop.Look
			meta.Summary = "Read the page's text, forms and state"
		default:
			meta.Class = desktop.Look
			meta.Summary = "Take a screenshot of the browser tab"
		}
	case browser.DragArgs:
		tabID = a.TabID
		meta.Summary = "Drag between page elements"
	case browser.RefArgs:
		tabID = a.TabID
		meta.Summary = "Click"
	case browser.TextArgs:
		tabID = a.TabID
		meta.Summary = fmt.Sprintf("Enter %d characters into", len([]rune(a.Text)))
	case browser.PressArgs:
		tabID = a.TabID
		meta.Summary = "Press " + a.Key + " in the page"
	case browser.ScrollArgs:
		tabID = a.TabID
		meta.Summary = "Scroll the page " + a.Direction
	case browser.WaitArgs:
		tabID = a.TabID
		meta.Class = desktop.Look
		meta.Summary = "Wait for a page element to become " + defaultBrowserState(a.State)
	case browser.SelectArgs:
		tabID = a.TabID
		meta.Summary = "Choose an option in"
	case browser.FormArgs:
		tabID = a.TabID
		meta.Summary = fmt.Sprintf("Fill %d form fields", len(a.Fields))
	case browser.UploadArgs:
		tabID = a.TabID
		meta.Summary = "Upload " + approvalBrowserText(a.File, 80) + " to the page"
	default:
		return browserPermission{}, errors.New("unsupported browser operation")
	}
	t, err := browserTab(ctx, tabID)
	if err != nil {
		return browserPermission{}, err
	}
	meta.App = browserApp(t.Host)
	if drag, ok := args.(browser.DragArgs); ok {
		from, err := browserCachedRef(tabID, drag.FromRef, t.Epoch)
		if err != nil {
			return browserPermission{}, err
		}
		to, err := browserCachedRef(tabID, drag.ToRef, t.Epoch)
		if err != nil {
			return browserPermission{}, err
		}
		meta.Summary = "Drag '" + approvalBrowserText(from.Name, 80) + "' to '" + approvalBrowserText(to.Name, 80) + "'"
	}
	if needsBrowserRef(call.Tool) || call.Tool == "browser_scroll" && refFromArgs(args) != "" {
		ref := refFromArgs(args)
		s, e := browserCachedRef(tabID, ref, t.Epoch)
		if e != nil {
			return browserPermission{}, e
		}
		meta.Summary += " '" + approvalBrowserText(s.Name, 80) + "'"
	}
	meta.Summary = fmt.Sprintf("%s in tab %d at %s", meta.Summary, tabID, browserApp(t.Host))
	return meta, nil
}

func browserCachedRef(tabID int, ref string, epoch uint64) (browserRef, error) {
	s := browserSessionFor(tabID)
	s.opMu.Lock()
	defer s.opMu.Unlock()
	item, ok := s.snapshot.refs[ref]
	if !ok || s.snapshot.nonce == "" || s.snapshot.epoch != epoch {
		return browserRef{}, errors.New("that page ref is stale; take a fresh browser snapshot")
	}
	return item, nil
}

func performBrowserTool(ctx context.Context, call browser.Call) (browser.Output, error) {
	args, err := decodeBrowserCall(call)
	if err != nil {
		return browser.Output{}, err
	}
	if call.Tool == "browser_tabs" {
		tabs, err := browserTabs(ctx)
		if err != nil {
			return browser.Output{}, err
		}
		return browserJSON(tabs), nil
	}
	if call.Tool == "browser_open_tab" {
		return browserJSONCall(func() (any, error) { return openBrowserTab(ctx, args.(browser.OpenTabArgs)) }), nil
	}
	if call.Tool == "browser_switch_tab" {
		return browserJSONCall(func() (any, error) { return switchBrowserTab(ctx, args.(browser.TabArgs)) }), nil
	}
	if call.Tool == "browser_close_tab" {
		return browserJSONCall(func() (any, error) { return closeBrowserTab(ctx, args.(browser.TabArgs)) }), nil
	}
	if call.Tool == "browser_navigate" {
		return browserJSONCall(func() (any, error) { return navigateBrowserTab(ctx, args.(browser.NavigateArgs)) }), nil
	}
	if call.Tool == "browser_back" || call.Tool == "browser_forward" || call.Tool == "browser_reload" {
		return browserJSONCall(func() (any, error) { return historyBrowserTab(ctx, args.(browser.TabArgs), call.Tool) }), nil
	}
	return performBrowserPageTool(ctx, call, args)
}

func browserJSONCall(fn func() (any, error)) browser.Output {
	v, err := fn()
	if err != nil {
		return browser.Output{Text: err.Error(), IsError: true}
	}
	return browserJSON(v)
}

func browserJSON(v any) browser.Output {
	b, err := json.Marshal(v)
	if err != nil {
		return browser.Output{Text: "Could not encode browser result.", IsError: true}
	}
	return browser.Output{Text: clipBrowser(string(b), maxBrowserText)}
}

func browserApp(host string) string {
	if host == "" {
		return "Phoenix browser"
	}
	return clipBrowser(host, 100)
}

func browserHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Host)
}

func safeTabURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return clipBrowser(u.String(), 300)
}

func clipBrowser(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return string(r)
}

func approvalBrowserText(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return clipBrowser(strings.Join(strings.Fields(s), " "), limit)
}

func defaultBrowserState(state string) string {
	if state == "" {
		return "visible"
	}
	return state
}

func decodeBrowserCall(call browser.Call) (any, error) {
	decode := func(v any) (any, error) {
		if len(call.Arguments) > browserRequestLimit {
			return nil, errors.New("browser arguments are too large")
		}
		if err := json.Unmarshal(call.Arguments, v); err != nil {
			return nil, errors.New("browser arguments are invalid")
		}
		return browserArgumentValue(v), nil
	}
	switch call.Tool {
	case "browser_tabs":
		return decode(&browser.NoArgs{})
	case "browser_snapshot":
		v, e := decode(&browser.SnapshotArgs{})
		if e == nil {
			a := v.(browser.SnapshotArgs)
			if !validTabID(a.TabID) || a.Depth < 0 || a.Depth > 12 || len(a.Query) > 200 {
				e = errors.New("invalid tab id, snapshot depth or query")
			}
		}
		return v, e
	case "browser_screenshot", "browser_read", "browser_copy", "browser_paste", "browser_back", "browser_forward", "browser_reload", "browser_close_tab", "browser_switch_tab":
		v, e := decode(&browser.TabArgs{})
		if e == nil && !validTabID(v.(browser.TabArgs).TabID) {
			e = errors.New("invalid browser tab id")
		}
		return v, e
	case "browser_navigate":
		v, e := decode(&browser.NavigateArgs{})
		if e == nil {
			a := v.(browser.NavigateArgs)
			if !validTabID(a.TabID) || !validBrowserURL(a.URL) {
				e = errors.New("navigate requires a tab and an http or https URL")
			}
		}
		return v, e
	case "browser_click":
		v, e := decode(&browser.RefArgs{})
		if e == nil && (!validTabID(v.(browser.RefArgs).TabID) || !validBrowserRef(v.(browser.RefArgs).Ref)) {
			e = errors.New("invalid tab id or page ref")
		}
		return v, e
	case "browser_extract":
		v, e := decode(&browser.ExtractArgs{})
		if e == nil {
			a := v.(browser.ExtractArgs)
			if !validTabID(a.TabID) || len(a.Query) > 200 || a.Offset < 0 || a.Limit < 0 || a.Limit > 100000 {
				e = errors.New("invalid extract request")
			}
		}
		return v, e
	case "browser_click_text":
		v, e := decode(&browser.ClickTextArgs{})
		if e == nil {
			a := v.(browser.ClickTextArgs)
			if !validTabID(a.TabID) || strings.TrimSpace(a.Text) == "" || len(a.Text) > 200 {
				e = errors.New("click_text needs a tab and the label to click")
			}
		}
		return v, e
	case "browser_drag":
		v, e := decode(&browser.DragArgs{})
		if e == nil {
			a := v.(browser.DragArgs)
			if !validTabID(a.TabID) || !validBrowserRef(a.FromRef) || !validBrowserRef(a.ToRef) {
				e = errors.New("invalid drag refs or browser tab")
			}
		}
		return v, e
	case "browser_type", "browser_fill":
		v, e := decode(&browser.TextArgs{})
		if e == nil {
			a := v.(browser.TextArgs)
			if !validTabID(a.TabID) || !validBrowserRef(a.Ref) || len([]rune(a.Text)) > 4000 || strings.ContainsRune(a.Text, 0) {
				e = errors.New("invalid tab, page ref, or text length")
			}
		}
		return v, e
	case "browser_press":
		v, e := decode(&browser.PressArgs{})
		if e == nil {
			a := v.(browser.PressArgs)
			if !validTabID(a.TabID) || !validBrowserKey(a.Key) {
				e = errors.New("unsupported key or invalid browser tab")
			}
		}
		return v, e
	case "browser_scroll":
		v, e := decode(&browser.ScrollArgs{})
		if e == nil {
			a := v.(browser.ScrollArgs)
			if !validTabID(a.TabID) || !validDirection(a.Direction) || !validOptionalRef(a.Ref) || a.Amount < 0 || a.Amount > 1200 {
				e = errors.New("invalid scroll request")
			}
		}
		return v, e
	case "browser_wait":
		v, e := decode(&browser.WaitArgs{})
		if e == nil {
			a := v.(browser.WaitArgs)
			if !validTabID(a.TabID) || len(a.Selector) == 0 || len(a.Selector) > 512 || (a.State != "" && a.State != "visible" && a.State != "hidden" && a.State != "attached") || a.Timeout < 0 || a.Timeout > 15000 {
				e = errors.New("invalid wait request")
			}
		}
		return v, e
	case "browser_select":
		v, e := decode(&browser.SelectArgs{})
		if e == nil {
			a := v.(browser.SelectArgs)
			if !validTabID(a.TabID) || !validBrowserRef(a.Ref) || len(a.Value) > 512 {
				e = errors.New("invalid select request")
			}
		}
		return v, e
	case "browser_fill_form":
		v, e := decode(&browser.FormArgs{})
		if e == nil {
			a := v.(browser.FormArgs)
			if !validTabID(a.TabID) || len(a.Fields) == 0 || len(a.Fields) > maxFormFields {
				e = errors.New("fill_form needs a tab and 1 to 40 fields")
			}
			for _, f := range a.Fields {
				if e == nil && (!validBrowserRef(f.Ref) || len([]rune(f.Value)) > 4000 || strings.ContainsRune(f.Value, 0)) {
					e = errors.New("invalid field ref or value")
				}
			}
		}
		return v, e
	case "browser_upload":
		v, e := decode(&browser.UploadArgs{})
		if e == nil {
			a := v.(browser.UploadArgs)
			if !validTabID(a.TabID) || strings.TrimSpace(a.File) == "" || len(a.File) > 300 || !validOptionalRef(a.Ref) {
				e = errors.New("upload needs a tab and a library file")
			}
		}
		return v, e
	case "browser_open_tab":
		v, e := decode(&browser.OpenTabArgs{})
		if e == nil {
			a := v.(browser.OpenTabArgs)
			if a.URL != "" && !validBrowserURL(a.URL) {
				e = errors.New("new tab URL must use http or https")
			}
		}
		return v, e
	default:
		return nil, errors.New("unknown browser operation")
	}
}

const browserRequestLimit = 32 << 10

func validTabID(id int) bool { return id > 0 }
func validBrowserRef(ref string) bool {
	return len(ref) >= 8 && len(ref) <= 40 && strings.HasPrefix(ref, "@s") && strings.Contains(ref, ":e")
}
func validOptionalRef(ref string) bool { return ref == "" || validBrowserRef(ref) }
func validDirection(v string) bool     { return v == "up" || v == "down" || v == "left" || v == "right" }

func validBrowserURL(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\x00") {
		return false
	}
	u, err := url.ParseRequestURI(raw)
	return err == nil && u.Host != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func needsBrowserRef(tool string) bool {
	switch tool {
	case "browser_click", "browser_type", "browser_fill", "browser_select":
		return true
	}
	return false
}

func refFromArgs(args any) string {
	switch a := args.(type) {
	case browser.RefArgs:
		return a.Ref
	case browser.TextArgs:
		return a.Ref
	case browser.SelectArgs:
		return a.Ref
	case browser.ScrollArgs:
		return a.Ref
	}
	return ""
}

func browserArgumentValue(v any) any {
	switch a := v.(type) {
	case *browser.NoArgs:
		return *a
	case *browser.TabArgs:
		return *a
	case *browser.SnapshotArgs:
		return *a
	case *browser.NavigateArgs:
		return *a
	case *browser.RefArgs:
		return *a
	case *browser.ClickTextArgs:
		return *a
	case *browser.ExtractArgs:
		return *a
	case *browser.DragArgs:
		return *a
	case *browser.TextArgs:
		return *a
	case *browser.PressArgs:
		return *a
	case *browser.ScrollArgs:
		return *a
	case *browser.WaitArgs:
		return *a
	case *browser.SelectArgs:
		return *a
	case *browser.OpenTabArgs:
		return *a
	case *browser.FormArgs:
		return *a
	case *browser.UploadArgs:
		return *a
	default:
		return v
	}
}

func randomSnapshotNonce() string {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func snapshotRef(nonce string, index int) string { return fmt.Sprintf("@s%s:e%d", nonce, index) }
