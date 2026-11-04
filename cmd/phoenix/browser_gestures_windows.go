package main

import (
	"context"
	"errors"
	"time"

	"phoenix/internal/browser"
)

// browserDrag uses the same snapshot and navigation epoch for both endpoints and
// always releases the mouse, including when navigation interrupts the operation.
func browserDrag(ctx context.Context, state browserTabState, from, to browserRef) (browser.Output, error) {
	center := func(ref browserRef) (float64, float64, error) {
		var box nodeBox
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.getBoxModel", map[string]any{"backendNodeId": ref.BackendNodeID}, &box); err != nil {
			return 0, 0, err
		}
		return quadCenter(box.Model.Content)
	}
	x1, y1, err := center(from)
	if err != nil {
		return browser.Output{}, err
	}
	x2, y2, err := center(to)
	if err != nil {
		return browser.Output{}, err
	}
	if showBrowserAgentPointer(ctx, state, x1, y1, "drag") {
		if err := browserPointerBeat(ctx); err != nil {
			return browser.Output{}, err
		}
	}
	if err := dispatchMouse(ctx, state, "mouseMoved", x1, y1, 0, 0, 0); err != nil {
		return browser.Output{}, err
	}
	pressErr := dispatchMouse(ctx, state, "mousePressed", x1, y1, 0, 0, 1)
	released := false
	defer func() {
		if released {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		state.Epoch = 0
		_ = dispatchMouse(releaseCtx, state, "mouseReleased", x2, y2, 0, 0, 1)
	}()
	if pressErr != nil {
		return browser.Output{}, pressErr
	}
	showBrowserAgentPointer(ctx, state, x2, y2, "drag")
	// Intermediate points give pages a chance to observe drag-start and drag-over.
	for step := 1; step <= 8; step++ {
		fraction := float64(step) / 8
		params := map[string]any{"type": "mouseMoved", "x": x1 + (x2-x1)*fraction, "y": y1 + (y2-y1)*fraction, "button": "none", "buttons": 1}
		if _, _, err := cdpCall(ctx, state.ID, state.Epoch, "Input.dispatchMouseEvent", params); err != nil {
			return browser.Output{}, err
		}
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	releaseState := state
	releaseState.Epoch = 0
	err = dispatchMouse(releaseCtx, releaseState, "mouseReleased", x2, y2, 0, 0, 1)
	released = true
	if err != nil {
		return browser.Output{}, err
	}
	return browser.Output{Text: "Dragged " + browserElementLabel(from) + " to " + browserElementLabel(to) + "."}, nil
}

func browserClipboardKey(ctx context.Context, state browserTabState, tool string) (browser.Output, error) {
	key, result := "Ctrl+C", "Sent Ctrl+C to the page."
	if tool == "browser_paste" {
		// Do not allow clipboard pastes into fields the normal fill tool refuses.
		var doc struct {
			Root struct {
				NodeID int `json:"nodeId"`
			} `json:"root"`
		}
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.getDocument", map[string]any{}, &doc); err != nil {
			return browser.Output{}, err
		}
		var focused struct {
			NodeID int `json:"nodeId"`
		}
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.querySelector", map[string]any{"nodeId": doc.Root.NodeID, "selector": ":focus"}, &focused); err != nil {
			return browser.Output{}, err
		}
		if focused.NodeID == 0 {
			return browser.Output{}, errors.New("focus an editable page field before pasting")
		}
		var desc domNodeDescription
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.describeNode", map[string]any{"nodeId": focused.NodeID}, &desc); err != nil {
			return browser.Output{}, err
		}
		if err := validateEditableNode(desc.Node); err != nil {
			return browser.Output{}, err
		}
		key, result = "Ctrl+V", "Sent Ctrl+V to the focused page field."
	}
	if point, ok := browserFocusedPoint(ctx, state); ok {
		action := "copy"
		if tool == "browser_paste" {
			action = "paste"
		}
		if showBrowserAgentPointer(ctx, state, point.X, point.Y, action) {
			if err := browserPointerBeat(ctx); err != nil {
				return browser.Output{}, err
			}
		}
		if err := dispatchMouse(ctx, state, "mouseMoved", point.X, point.Y, 0, 0, 0); err != nil {
			return browser.Output{}, err
		}
	}
	if err := dispatchKey(ctx, state, browser.PressArgs{Key: key}); err != nil {
		return browser.Output{}, err
	}
	return browser.Output{Text: result}, nil
}
