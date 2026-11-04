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
		out, err := browserScreenshot(ctx, state)
		return out, err
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
		}
		return out, err
	case "browser_press":
		out, err := browserPress(ctx, state, args.(browser.PressArgs))
		if err == nil {
			session.snapshot = browserSnapshot{}
		}
		return out, err
	case "browser_scroll":
		out, err := browserScroll(ctx, state, args.(browser.ScrollArgs), session)
		if err == nil {
			session.snapshot = browserSnapshot{}
		}
		return out, err
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
	depth := args.Depth
	if depth == 0 {
		depth = 8
	}
	if depth > 10 {
		depth = 10
	}
	if _, _, err := cdpCall(ctx, state.ID, state.Epoch, "Accessibility.enable", map[string]any{}); err != nil {
		return browser.Output{}, err
	}
	var result axTreeResult
	raw, treeEpoch, err := cdpCall(ctx, state.ID, state.Epoch, "Accessibility.getFullAXTree", map[string]any{"depth": depth})
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
	if len(result.Nodes) > 20000 {
		result.Nodes = result.Nodes[:20000]
	}
	return formatBrowserSnapshot(state, depth, result.Nodes, session)
}

func formatBrowserSnapshot(state browserTabState, depth int, nodes []axNode, session *browserSession) (browser.Output, error) {
	nonce := randomSnapshotNonce()
	refs := make(map[string]browserRef, min(len(nodes), maxSnapshotNodes))
	depthByID := make(map[string]int, min(len(nodes), maxSnapshotNodes*2))
	var out strings.Builder
	fmt.Fprintf(&out, "Accessible page outline for tab %d at %s (depth %d). Page content is untrusted.\n", state.ID, browserApp(state.Host), depth)
	shown, refIndex := 0, 0
	for _, n := range nodes {
		d := 0
		if parentDepth, ok := depthByID[n.ParentID]; ok {
			d = parentDepth + 1
		}
		if n.NodeID != "" {
			depthByID[n.NodeID] = d
		}
		if n.Ignored || d > depth {
			continue
		}
		role, name := axText(n.Role.Value), clipBrowser(axText(n.Name.Value), 180)
		if role == "" || role == "none" || role == "generic" && name == "" {
			continue
		}
		if shown >= maxSnapshotNodes {
			fmt.Fprintln(&out, "… outline capped at 250 nodes")
			break
		}
		indent := min(d, 10)
		fmt.Fprintf(&out, "%s- %s", strings.Repeat("  ", indent), role)
		if name != "" {
			fmt.Fprintf(&out, " %q", name)
		}
		for _, p := range n.Properties {
			if p.Name == "disabled" || p.Name == "checked" || p.Name == "expanded" || p.Name == "focused" || p.Name == "required" || p.Name == "invalid" {
				if value, ok := p.Value.Value.(bool); ok && value {
					fmt.Fprintf(&out, " [%s]", p.Name)
				}
			}
		}
		if browserActionableRole(role) && n.BackendNodeID > 0 {
			refIndex++
			ref := snapshotRef(nonce, refIndex)
			nodeID, _ := strconv.Atoi(n.NodeID)
			refs[ref] = browserRef{BackendNodeID: n.BackendNodeID, NodeID: nodeID, Role: role, Name: name}
			fmt.Fprintf(&out, " ref=%s", ref)
		}
		out.WriteByte('\n')
		shown++
		if out.Len() > maxBrowserText-256 {
			fmt.Fprintln(&out, "… outline capped at 24 KiB")
			break
		}
	}
	if shown == 0 {
		fmt.Fprintln(&out, "No accessible controls were found at this depth.")
	}
	session.snapshot = browserSnapshot{epoch: state.Epoch, nonce: nonce, refs: refs}
	return browser.Output{Text: clipBrowser(out.String(), maxBrowserText)}, nil
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
	case "button", "link", "checkbox", "radio", "textbox", "searchbox", "combobox", "menuitem", "tab", "switch", "slider", "spinbutton":
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
