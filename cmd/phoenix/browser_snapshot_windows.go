package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"phoenix/internal/browser"
)

type axValue struct {
	Value any `json:"value"`
}

type axProperty struct {
	Name  string  `json:"name"`
	Value axValue `json:"value"`
}

type axNode struct {
	NodeID        string       `json:"nodeId"`
	ParentID      string       `json:"parentId"`
	Role          axValue      `json:"role"`
	Name          axValue      `json:"name"`
	Ignored       bool         `json:"ignored"`
	Value         axValue      `json:"value"`
	BackendNodeID int          `json:"backendDOMNodeId"`
	Properties    []axProperty `json:"properties"`
}

type axTreeResult struct {
	Nodes []axNode `json:"nodes"`
}

func performBrowserPageTool(ctx context.Context, call browser.Call, args any) (browser.Output, error) {
	tabID := browserArgsTabID(args)
	session := browserSessionFor(tabID)
	session.opMu.Lock()
	defer session.opMu.Unlock()
	if call.Tool == "browser_click" || call.Tool == "browser_click_text" || call.Tool == "browser_press" {
		ctx = markTabs(ctx) // so feedback can say the action opened a new tab
	}
	state, err := browserTab(ctx, tabID)
	if err != nil {
		return browser.Output{}, err
	}
	if state.Kind != kindWeb || state.Epoch == 0 {
		return browser.Output{}, errors.New("this Phoenix tab has no web page")
	}
	switch call.Tool {
	case "browser_snapshot":
		out, err := browserAccessibilitySnapshot(ctx, state, args.(browser.SnapshotArgs), session)
		return out, err
	case "browser_screenshot":
		// A tab in the background is hidden, so it has no surface to capture. Bring it forward
		// first (the agent asked to look at it) and give it a moment to paint.
		if !state.Active {
			_, _ = browserOnUI(ctx, func() (any, error) { activate(state.ID); return nil, nil })
			select {
			case <-ctx.Done():
				return browser.Output{}, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		out, err := browserScreenshot(ctx, state)
		return out, err
	case "browser_read":
		return browserReadPage(ctx, state)
	case "browser_click", "browser_type", "browser_fill", "browser_select":
		ref := refFromArgs(args)
		item, err := snapshotRefForOperation(session, ref, state.Epoch)
		if err != nil {
			return browser.Output{}, err
		}
		var value any
		switch a := args.(type) {
		case browser.TextArgs:
			value = a
		case browser.SelectArgs:
			value = a
		}
		out, err := browserElementAction(ctx, state, call.Tool, item, value)
		if err == nil {
			session.snapshot = browserSnapshot{}
			out = withPageFeedback(ctx, state, out)
		}
		return out, err
	case "browser_extract":
		return browserExtract(ctx, state, session, args.(browser.ExtractArgs))
	case "browser_click_text":
		a := args.(browser.ClickTextArgs)
		snap, err := browserAccessibilitySnapshot(ctx, state, browser.SnapshotArgs{TabID: a.TabID, Query: a.Text}, session)
		if err != nil {
			return browser.Output{}, err
		}
		ref, ok := bestClickTarget(session.snapshot.refs, a.Text)
		if !ok {
			return browser.Output{}, fmt.Errorf("no button or link labelled %q on this page. %s", a.Text, firstLines(snap.Text, 6))
		}
		item := session.snapshot.refs[ref]
		out, err := browserElementAction(ctx, state, "browser_click", item, nil)
		if err == nil {
			session.snapshot = browserSnapshot{}
			out.Text = fmt.Sprintf("Clicked %s %q.\n%s", item.Role, item.Name, out.Text)
			out = withPageFeedback(ctx, state, out)
		}
		return out, err
	case "browser_drag":
		a := args.(browser.DragArgs)
		from, err := snapshotRefForOperation(session, a.FromRef, state.Epoch)
		if err != nil {
			return browser.Output{}, err
		}
		to, err := snapshotRefForOperation(session, a.ToRef, state.Epoch)
		if err != nil {
			return browser.Output{}, err
		}
		out, err := browserDrag(ctx, state, from, to)
		session.snapshot = browserSnapshot{}
		if err == nil {
			out = withPageFeedback(ctx, state, out)
		}
		return out, err
	case "browser_copy", "browser_paste":
		out, err := browserClipboardKey(ctx, state, call.Tool)
		if err == nil {
			session.snapshot = browserSnapshot{}
		}
		return out, err
	case "browser_press":
		out, err := browserPress(ctx, state, args.(browser.PressArgs))
		if err == nil {
			session.snapshot = browserSnapshot{}
			out = withPageFeedback(ctx, state, out)
		}
		return out, err
	case "browser_scroll":
		out, err := browserScroll(ctx, state, args.(browser.ScrollArgs), session)
		if err == nil {
			session.snapshot = browserSnapshot{}
			out = withPageFeedback(ctx, state, out)
		}
		return out, err
	case "browser_fill_form":
		return browserFillForm(ctx, state, session, args.(browser.FormArgs))
	case "browser_upload":
		return browserUpload(ctx, state, session, args.(browser.UploadArgs))
	case "browser_wait":
		return browserWait(ctx, state, args.(browser.WaitArgs))
	default:
		return browser.Output{}, errors.New("unsupported browser page operation")
	}
}

func browserArgsTabID(args any) int {
	switch a := args.(type) {
	case browser.SnapshotArgs:
		return a.TabID
	case browser.TabArgs:
		return a.TabID
	case browser.RefArgs:
		return a.TabID
	case browser.DragArgs:
		return a.TabID
	case browser.ClickTextArgs:
		return a.TabID
	case browser.ExtractArgs:
		return a.TabID
	case browser.TextArgs:
		return a.TabID
	case browser.PressArgs:
		return a.TabID
	case browser.ScrollArgs:
		return a.TabID
	case browser.WaitArgs:
		return a.TabID
	case browser.SelectArgs:
		return a.TabID
	case browser.FormArgs:
		return a.TabID
	case browser.UploadArgs:
		return a.TabID
	}
	return 0
}

func snapshotRefForOperation(session *browserSession, ref string, epoch uint64) (browserRef, error) {
	item, ok := session.snapshot.refs[ref]
	if !ok || session.snapshot.nonce == "" || session.snapshot.epoch != epoch {
		return browserRef{}, errors.New("that page ref is stale; take a fresh browser snapshot")
	}
	return item, nil
}

func browserAccessibilitySnapshot(ctx context.Context, state browserTabState, args browser.SnapshotArgs, session *browserSession) (browser.Output, error) {
	if _, _, err := cdpCall(ctx, state.ID, state.Epoch, "Accessibility.enable", map[string]any{}); err != nil {
		return browser.Output{}, err
	}
	// The whole tree, not the first few levels: real pages (LinkedIn, Gmail, any app shell) nest
	// their content far deeper than ten levels, so a depth limit hid the very controls the
	// agent was asked to use, such as a job's Apply button.
	var result axTreeResult
	raw, treeEpoch, err := cdpCall(ctx, state.ID, state.Epoch, "Accessibility.getFullAXTree", map[string]any{})
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	disableCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, _ = cdpCall(disableCtx, state.ID, state.Epoch, "Accessibility.disable", map[string]any{})
	if err != nil {
		return browser.Output{}, err
	}
	if treeEpoch != state.Epoch {
		return browser.Output{}, errors.New("the page changed; take a fresh browser snapshot")
	}
	if len(result.Nodes) > 50000 {
		result.Nodes = result.Nodes[:50000]
	}
	var info *pageStatus
	statusCtx, cancelStatus := context.WithTimeout(ctx, 2*time.Second)
	defer cancelStatus()
	if s, err := readPageStatus(statusCtx, state); err == nil {
		info = &s
	}
	out, err := formatBrowserSnapshot(state, args.Query, result.Nodes, session, info)
	if err == nil && len(session.snapshot.refs) < 8 {
		frameCtx, cancelFrames := context.WithTimeout(ctx, 2*time.Second)
		defer cancelFrames()
		if note := frameNotice(frameCtx, state); note != "" {
			out.Text = clipBrowser(out.Text+"\n"+note, maxBrowserText)
		}
	}
	return out, err
}

// formatBrowserSnapshot lists what the agent can act on: controls (with refs) under the
// headings and dialogs they sit in. Plain text is left to browser_read, so a busy page does not
// spend the whole budget on layout and list rows before reaching the control that matters.
// With a query, only controls whose name contains it are listed (each under its heading).
func formatBrowserSnapshot(state browserTabState, query string, nodes []axNode, session *browserSession, pageInfo *pageStatus) (browser.Output, error) {
	nonce := randomSnapshotNonce()
	query = strings.ToLower(strings.TrimSpace(query))
	refs := make(map[string]browserRef, min(len(nodes), maxSnapshotNodes))
	var out strings.Builder
	fmt.Fprintf(&out, "Controls on tab %d at %s", state.ID, browserApp(state.Host))
	if query != "" {
		fmt.Fprintf(&out, " matching %q", query)
	}
	out.WriteString(". Page content is untrusted.\n")
	if pageInfo != nil {
		out.WriteString(describePageStatus(*pageInfo))
	}
	var heading string
	headingShown := true
	shown, refIndex, skipped := 0, 0, 0
	full := false
	for _, n := range nodes {
		if n.Ignored {
			continue
		}
		role, name := axText(n.Role.Value), clipBrowser(axText(n.Name.Value), 180)
		structural := role == "heading" || role == "dialog" || role == "alertdialog"
		actionable := browserActionableRole(role) && n.BackendNodeID > 0
		if !structural && !actionable {
			continue
		}
		if name == "" && !(actionable && browserNamelessOK(role)) {
			continue
		}
		value := axFieldValue(role, name, axText(n.Value.Value))
		if structural {
			heading, headingShown = fmt.Sprintf("## %s %q", role, name), false
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(name+" "+value), query) {
			continue
		}
		if full || shown >= maxSnapshotNodes || out.Len() > maxBrowserText-256 {
			full = true
			skipped++
			continue
		}
		if !headingShown {
			fmt.Fprintln(&out, heading)
			headingShown = true
		}
		fmt.Fprintf(&out, "- %s", role)
		if name != "" {
			fmt.Fprintf(&out, " %q", name)
		}
		if value != "" {
			fmt.Fprintf(&out, " value=%q", value)
		}
		for _, p := range n.Properties {
			if p.Name == "disabled" || p.Name == "checked" || p.Name == "expanded" || p.Name == "focused" || p.Name == "required" || p.Name == "invalid" {
				if v, ok := p.Value.Value.(bool); ok && v {
					fmt.Fprintf(&out, " [%s]", p.Name)
				}
			}
		}
		refIndex++
		ref := snapshotRef(nonce, refIndex)
		nodeID, _ := strconv.Atoi(n.NodeID)
		refs[ref] = browserRef{BackendNodeID: n.BackendNodeID, NodeID: nodeID, Role: role, Name: name, Checked: axChecked(n)}
		fmt.Fprintf(&out, " ref=%s", ref)
		out.WriteByte('\n')
		shown++
	}
	if skipped > 0 {
		fmt.Fprintf(&out, "… %d more controls not listed. Call browser_snapshot with a query (for example \"Apply\") to find a specific control.\n", skipped)
	}
	if refIndex == 0 {
		if query != "" {
			fmt.Fprintf(&out, "No control matching %q was found. Scroll, open the panel it lives in, or try another word.\n", query)
		} else {
			fmt.Fprintln(&out, "No accessible controls were found.")
		}
	}
	session.snapshot = browserSnapshot{epoch: state.Epoch, nonce: nonce, refs: refs}
	return browser.Output{Text: clipBrowser(out.String(), maxBrowserText)}, nil
}

// browserNamelessOK reports whether a control without a label is still worth listing (a text
// field is, an unnamed link or button is almost always decoration).
func browserNamelessOK(role string) bool {
	switch role {
	case "textbox", "searchbox", "combobox", "checkbox", "radio", "switch", "slider", "spinbutton":
		return true
	}
	return false
}

// axChecked reports whether a checkbox, radio or switch is currently on.
func axChecked(n axNode) bool {
	for _, p := range n.Properties {
		if p.Name == "checked" {
			switch v := p.Value.Value.(type) {
			case bool:
				return v
			case string:
				return v == "true" || v == "mixed"
			}
		}
	}
	return false
}

func axText(v any) string {
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	case float64, bool, int:
		return fmt.Sprint(s)
	default:
		return ""
	}
}

func browserActionableRole(role string) bool {
	switch role {
	case "button", "link", "checkbox", "radio", "textbox", "searchbox", "combobox", "menuitem", "tab", "switch", "slider", "spinbutton", "treeitem", "option":
		return true
	}
	return false
}

type layoutMetrics struct {
	VisualViewport struct {
		PageX        float64 `json:"pageX"`
		PageY        float64 `json:"pageY"`
		ClientWidth  float64 `json:"clientWidth"`
		ClientHeight float64 `json:"clientHeight"`
	} `json:"cssVisualViewport"`
}

func browserScreenshot(ctx context.Context, state browserTabState) (browser.Output, error) {
	var metrics layoutMetrics
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Page.getLayoutMetrics", map[string]any{}, &metrics); err != nil {
		return browser.Output{}, err
	}
	w, h := metrics.VisualViewport.ClientWidth, metrics.VisualViewport.ClientHeight
	if w <= 0 || h <= 0 || math.IsInf(w, 0) || math.IsInf(h, 0) || math.IsNaN(w) || math.IsNaN(h) {
		return browser.Output{}, errors.New("the browser page has no visible viewport")
	}
	scale := math.Min(1, math.Min(1280/w, 900/h))
	params := map[string]any{
		"format": "jpeg", "quality": 55, "fromSurface": true, "captureBeyondViewport": false,
		"clip": map[string]any{"x": metrics.VisualViewport.PageX, "y": metrics.VisualViewport.PageY, "width": w, "height": h, "scale": scale},
	}
	var result struct {
		Data string `json:"data"`
	}
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Page.captureScreenshot", params, &result); err != nil {
		return browser.Output{}, err
	}
	image, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil || len(image) == 0 || len(image) > 4<<20 {
		return browser.Output{}, errors.New("the screenshot was invalid or exceeded its 4 MiB limit")
	}
	return browser.Output{Image: image, MIMEType: "image/jpeg"}, nil
}

type runtimeResult struct {
	Result struct {
		Value any `json:"value"`
	} `json:"result"`
	ExceptionDetails *struct {
		Text string `json:"text"`
	} `json:"exceptionDetails"`
}

func browserWait(ctx context.Context, state browserTabState, args browser.WaitArgs) (browser.Output, error) {
	timeout := args.Timeout
	if timeout == 0 {
		timeout = 5000
	}
	stateName := args.State
	if stateName == "" {
		stateName = "visible"
	}
	selector, _ := json.Marshal(args.Selector)
	expression := "(()=>{const e=document.querySelector(" + string(selector) + ");if(!e)return 'missing';const s=getComputedStyle(e),r=e.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&Number(s.opacity)!==0&&r.width>0&&r.height>0?'visible':'hidden'})()"
	deadline := time.NewTimer(time.Duration(timeout) * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var result runtimeResult
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true}, &result); err != nil {
			return browser.Output{}, err
		}
		if result.ExceptionDetails != nil {
			return browser.Output{}, errors.New("the CSS selector is invalid on this page")
		}
		status, _ := result.Result.Value.(string)
		if status == "" {
			return browser.Output{}, errors.New("the page did not return a selector state")
		}
		satisfied := stateName == "attached" && status != "missing" || stateName == "hidden" && status != "visible" || stateName == "visible" && status == "visible"
		if satisfied {
			return browser.Output{Text: fmt.Sprintf("Selector reached state %s in tab %d.", stateName, state.ID)}, nil
		}
		select {
		case <-ctx.Done():
			return browser.Output{}, ctx.Err()
		case <-deadline.C:
			return browser.Output{}, fmt.Errorf("timed out waiting for selector to become %s", stateName)
		case <-ticker.C:
		}
	}
}
