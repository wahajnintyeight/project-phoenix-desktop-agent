package main

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"
)

type browserPointerPosition struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Action string  `json:"action"`
	Label  string  `json:"label,omitempty"` // a key name or scroll direction, never page text
}

type browserPagePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// showBrowserAgentPointer draws an ephemeral cursor inside the actual page. Browser input is
// dispatched through CDP, which moves the page's input target without moving the Windows cursor.
func showBrowserAgentPointer(ctx context.Context, state browserTabState, x, y float64, action string) bool {
	return showBrowserAgentPointerLabeled(ctx, state, x, y, action, "")
}

// showBrowserAgentPointerLabeled is showBrowserAgentPointer with a short label for the action
// chip: the key being pressed or the direction being scrolled. Both come from validated input.
func showBrowserAgentPointerLabeled(ctx context.Context, state browserTabState, x, y float64, action, label string) bool {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return false
	}
	args, err := json.Marshal(browserPointerPosition{X: x, Y: y, Action: action, Label: label})
	if err != nil {
		return false
	}
	expression := strings.Replace(browserPointerScript, "__PHOENIX_POINTER_ARGS__", string(args), 1)
	_, _, err = cdpCall(ctx, state.ID, state.Epoch, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true})
	return err == nil
}

// Give the cursor time to glide to its target and the halo to lock on before the matching
// input event is sent, so a person can follow what is about to happen.
func browserPointerBeat(ctx context.Context) error {
	timer := time.NewTimer(280 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// browserFocusedPoint locates the current selection or focused control without reading its
// text, so keyboard and clipboard operations can be shown at their actual page target.
func browserFocusedPoint(ctx context.Context, state browserTabState) (browserPagePoint, bool) {
	const expression = `(()=>{let r=null;const s=getSelection();if(s&&s.rangeCount&&s.toString()){const q=s.getRangeAt(0).getBoundingClientRect();if(q.width&&q.height)r=q}if(!r){const e=document.activeElement;if(e&&e!==document.body&&e!==document.documentElement)r=e.getBoundingClientRect()}if(!r||!r.width||!r.height)return null;return{x:r.left+r.width/2,y:r.top+r.height/2}})()`
	var result struct {
		Result struct {
			Value *browserPagePoint `json:"value"`
		} `json:"result"`
	}
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true}, &result); err != nil || result.Result.Value == nil {
		return browserPagePoint{}, false
	}
	return *result.Result.Value, true
}
