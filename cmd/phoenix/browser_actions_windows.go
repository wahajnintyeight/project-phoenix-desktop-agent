package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"phoenix/internal/browser"
)

type domNode struct {
	NodeID     int      `json:"nodeId"`
	NodeName   string   `json:"nodeName"`
	LocalName  string   `json:"localName"`
	Attributes []string `json:"attributes"`
}

type domNodeDescription struct {
	Node domNode `json:"node"`
}

type nodeBox struct {
	Model struct {
		Content []float64 `json:"content"`
	} `json:"model"`
}

func browserElementAction(ctx context.Context, state browserTabState, tool string, ref browserRef, arg any) (browser.Output, error) {
	switch tool {
	case "browser_click":
		if !browserClickableRole(ref.Role) {
			return browser.Output{}, errors.New("that ref is not a clickable control")
		}
		return browserClick(ctx, state, ref)
	case "browser_type", "browser_fill":
		if !browserEditableRole(ref.Role) {
			return browser.Output{}, errors.New("that ref is not an editable text field")
		}
		return browserEnterText(ctx, state, ref, arg.(browser.TextArgs).Text, tool == "browser_fill")
	case "browser_select":
		if ref.Role != "combobox" && ref.Role != "listbox" {
			return browser.Output{}, errors.New("that ref is not a select control")
		}
		return browserSelect(ctx, state, ref, arg.(browser.SelectArgs).Value)
	default:
		return browser.Output{}, errors.New("unsupported page action")
	}
}

func browserClickableRole(role string) bool {
	switch role {
	case "button", "link", "checkbox", "radio", "menuitem", "tab", "switch", "option":
		return true
	}
	return false
}

func browserEditableRole(role string) bool {
	switch role {
	case "textbox", "searchbox":
		return true
	}
	return false
}

func browserClick(ctx context.Context, state browserTabState, ref browserRef) (browser.Output, error) {
	if _, _, err := cdpCall(ctx, state.ID, state.Epoch, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": ref.BackendNodeID}); err != nil {
		return browser.Output{}, err
	}
	var box nodeBox
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.getBoxModel", map[string]any{"backendNodeId": ref.BackendNodeID}, &box); err != nil {
		return browser.Output{}, err
	}
	x, y, err := quadCenter(box.Model.Content)
	if err != nil {
		return browser.Output{}, err
	}
	pressErr := dispatchMouse(ctx, state, "mousePressed", x, y, 0, 0, 1)
	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	releaseState := state
	releaseState.Epoch = 0 // release even if the click navigated or the request was canceled
	releaseErr := dispatchMouse(releaseCtx, releaseState, "mouseReleased", x, y, 0, 0, 1)
	if pressErr != nil {
		return browser.Output{}, pressErr
	}
	if releaseErr != nil {
		return browser.Output{}, releaseErr
	}
	return browser.Output{Text: "Clicked " + browserElementLabel(ref) + "."}, nil
}

func browserEnterText(ctx context.Context, state browserTabState, ref browserRef, text string, replace bool) (browser.Output, error) {
	var desc domNodeDescription
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.describeNode", map[string]any{"backendNodeId": ref.BackendNodeID}, &desc); err != nil {
		return browser.Output{}, err
	}
	if err := validateEditableNode(desc.Node); err != nil {
		return browser.Output{}, err
	}
	if desc.Node.NodeID == 0 {
		return browser.Output{}, errors.New("the page field is no longer available")
	}
	if _, _, err := cdpCall(ctx, state.ID, state.Epoch, "DOM.focus", map[string]any{"nodeId": desc.Node.NodeID}); err != nil {
		return browser.Output{}, err
	}
	if replace {
		if err := dispatchKey(ctx, state, browser.PressArgs{Key: "Ctrl+A"}); err != nil {
			return browser.Output{}, err
		}
	}
	if _, _, err := cdpCall(ctx, state.ID, state.Epoch, "Input.insertText", map[string]any{"text": text}); err != nil {
		return browser.Output{}, err
	}
	action := "Typed into "
	if replace {
		action = "Filled "
	}
	return browser.Output{Text: action + browserElementLabel(ref) + "."}, nil
}

func validateEditableNode(node domNode) error {
	attrs := make(map[string]string, len(node.Attributes)/2)
	for i := 0; i+1 < len(node.Attributes); i += 2 {
		attrs[strings.ToLower(node.Attributes[i])] = strings.ToLower(node.Attributes[i+1])
	}
	local := strings.ToLower(node.LocalName)
	switch local {
	case "input":
		typ := attrs["type"]
		if typ == "" {
			typ = "text"
		}
		switch typ {
		case "text", "search", "email", "tel", "url", "number", "date", "datetime-local", "month", "week", "time":
		default:
			return errors.New("Phoenix will not type into password, file, hidden, or non-text inputs")
		}
	case "textarea":
	case "":
		if attrs["contenteditable"] != "true" && attrs["contenteditable"] != "" {
			return errors.New("the page field is not editable")
		}
		if attrs["contenteditable"] == "" {
			return errors.New("the page field is not an editable text field")
		}
	default:
		if attrs["contenteditable"] != "true" {
			return errors.New("the page field is not an editable text field")
		}
	}
	if strings.Contains(attrs["autocomplete"], "password") || strings.Contains(attrs["autocomplete"], "one-time-code") || strings.Contains(attrs["autocomplete"], "cc-") {
		return errors.New("Phoenix will not type passwords, payment data, or authentication codes")
	}
	for _, key := range []string{"name", "id", "placeholder", "aria-label"} {
		label := strings.NewReplacer("_", " ", "-", " ").Replace(attrs[key])
		for _, sensitive := range []string{"password", "one time code", "verification code", "security code", "card number", "credit card", "cvv", "cvc", "cc number"} {
			if strings.Contains(label, sensitive) {
				return errors.New("Phoenix will not type into a field that appears to request a password, payment detail, or authentication code")
			}
		}
	}
	return nil
}

func browserSelect(ctx context.Context, state browserTabState, ref browserRef, value string) (browser.Output, error) {
	var desc domNodeDescription
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.describeNode", map[string]any{"backendNodeId": ref.BackendNodeID}, &desc); err != nil {
		return browser.Output{}, err
	}
	if strings.ToLower(desc.Node.LocalName) != "select" || desc.Node.NodeID == 0 {
		return browser.Output{}, errors.New("that page control is not a native select element")
	}
	var resolved struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.resolveNode", map[string]any{"backendNodeId": ref.BackendNodeID}, &resolved); err != nil {
		return browser.Output{}, err
	}
	function := `function(v){const o=Array.from(this.options).find(x=>x.value===v);if(!o)throw new Error("option not found");this.value=v;this.dispatchEvent(new Event("input",{bubbles:true}));this.dispatchEvent(new Event("change",{bubbles:true}));return true}`
	var callResult runtimeResult
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Runtime.callFunctionOn", map[string]any{
		"objectId": resolved.Object.ObjectID, "functionDeclaration": function,
		"arguments": []any{map[string]any{"value": value}}, "returnByValue": true,
	}, &callResult); err != nil {
		return browser.Output{}, err
	}
	if callResult.ExceptionDetails != nil {
		return browser.Output{}, errors.New("that option is not available in the page control")
	}
	return browser.Output{Text: "Selected an option in " + browserElementLabel(ref) + "."}, nil
}

func browserPress(ctx context.Context, state browserTabState, args browser.PressArgs) (browser.Output, error) {
	if err := dispatchKey(ctx, state, args); err != nil {
		return browser.Output{}, err
	}
	return browser.Output{Text: "Pressed " + args.Key + " in the page."}, nil
}

func dispatchKey(ctx context.Context, state browserTabState, args browser.PressArgs) error {
	key, modifiers, code, virtualKey, err := browserKeySpec(args.Key)
	if err != nil {
		return err
	}
	params := map[string]any{"key": key, "code": code, "windowsVirtualKeyCode": virtualKey, "nativeVirtualKeyCode": virtualKey, "modifiers": modifiers}
	params["type"] = "keyDown"
	_, _, keyDownErr := cdpCall(ctx, state.ID, state.Epoch, "Input.dispatchKeyEvent", params)
	params["type"] = "keyUp"
	releaseState := state
	releaseState.Epoch = 0 // never leave a key held if navigation or cancellation races the call
	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, keyUpErr := cdpCall(releaseCtx, releaseState.ID, releaseState.Epoch, "Input.dispatchKeyEvent", params)
	if keyDownErr != nil {
		return keyDownErr
	}
	return keyUpErr
}

func validBrowserKey(key string) bool {
	if strings.EqualFold(key, "Ctrl+C") || strings.EqualFold(key, "Ctrl+V") {
		return false
	}
	_, _, _, _, err := browserKeySpec(key)
	return err == nil
}

func browserKeySpec(raw string) (key string, modifiers int, code string, vk int, err error) {
	parts := strings.Split(raw, "+")
	if len(parts) == 2 {
		switch parts[0] {
		case "Ctrl":
			modifiers = 2
		case "Shift":
			modifiers = 8
		default:
			return "", 0, "", 0, errors.New("unsupported key modifier")
		}
		raw = parts[1]
	} else if len(parts) != 1 {
		return "", 0, "", 0, errors.New("unsupported key chord")
	}
	switch raw {
	case "Enter":
		key, code, vk = "Enter", "Enter", 13
	case "Tab":
		key, code, vk = "Tab", "Tab", 9
	case "Escape":
		key, code, vk = "Escape", "Escape", 27
	case "ArrowUp":
		key, code, vk = "ArrowUp", "ArrowUp", 38
	case "ArrowDown":
		key, code, vk = "ArrowDown", "ArrowDown", 40
	case "ArrowLeft":
		key, code, vk = "ArrowLeft", "ArrowLeft", 37
	case "ArrowRight":
		key, code, vk = "ArrowRight", "ArrowRight", 39
	case "Backspace":
		key, code, vk = "Backspace", "Backspace", 8
	case "Delete":
		key, code, vk = "Delete", "Delete", 46
	case "Home":
		key, code, vk = "Home", "Home", 36
	case "End":
		key, code, vk = "End", "End", 35
	case "PageUp":
		key, code, vk = "PageUp", "PageUp", 33
	case "PageDown":
		key, code, vk = "PageDown", "PageDown", 34
	case "Space":
		key, code, vk = " ", "Space", 32
	case "a", "A", "c", "C", "v", "V":
		if modifiers != 2 {
			return "", 0, "", 0, errors.New("only Ctrl+A, Ctrl+C and Ctrl+V are supported for letters")
		}
		key = strings.ToLower(raw)
		code = "Key" + strings.ToUpper(raw)
		vk = int(strings.ToUpper(raw)[0])
	default:
		return "", 0, "", 0, errors.New("unsupported key")
	}
	if modifiers != 0 && key != "a" && key != "c" && key != "v" && !(modifiers == 8 && key == "Tab") {
		return "", 0, "", 0, errors.New("that key chord is not supported")
	}
	return key, modifiers, code, vk, nil
}

func browserScroll(ctx context.Context, state browserTabState, args browser.ScrollArgs, session *browserSession) (browser.Output, error) {
	x, y := 1.0, 1.0
	if args.Ref != "" {
		ref, err := snapshotRefForOperation(session, args.Ref, state.Epoch)
		if err != nil {
			return browser.Output{}, err
		}
		if _, _, err := cdpCall(ctx, state.ID, state.Epoch, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": ref.BackendNodeID}); err != nil {
			return browser.Output{}, err
		}
		var box nodeBox
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.getBoxModel", map[string]any{"backendNodeId": ref.BackendNodeID}, &box); err != nil {
			return browser.Output{}, err
		}
		x, y, err = quadCenter(box.Model.Content)
		if err != nil {
			return browser.Output{}, err
		}
	} else {
		var metrics layoutMetrics
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Page.getLayoutMetrics", map[string]any{}, &metrics); err != nil {
			return browser.Output{}, err
		}
		x = metrics.VisualViewport.PageX + metrics.VisualViewport.ClientWidth/2
		y = metrics.VisualViewport.PageY + metrics.VisualViewport.ClientHeight/2
	}
	amount := args.Amount
	if amount == 0 {
		amount = 600
	}
	dx, dy := 0.0, 0.0
	switch args.Direction {
	case "up":
		dy = -float64(amount)
	case "down":
		dy = float64(amount)
	case "left":
		dx = -float64(amount)
	case "right":
		dx = float64(amount)
	}
	if err := dispatchMouse(ctx, state, "mouseWheel", x, y, dx, dy, 0); err != nil {
		return browser.Output{}, err
	}
	return browser.Output{Text: "Scrolled " + args.Direction + "."}, nil
}

func dispatchMouse(ctx context.Context, state browserTabState, event string, x, y, dx, dy float64, button int) error {
	params := map[string]any{"type": event, "x": x, "y": y, "button": "left", "clickCount": 1}
	if event == "mousePressed" && button != 0 {
		params["buttons"] = 1
	}
	if event == "mouseWheel" {
		params["button"], params["deltaX"], params["deltaY"] = "none", dx, dy
	} else if button == 0 {
		params["button"], params["clickCount"] = "none", 0
	}
	_, _, err := cdpCall(ctx, state.ID, state.Epoch, "Input.dispatchMouseEvent", params)
	return err
}

func quadCenter(quad []float64) (float64, float64, error) {
	if len(quad) != 8 {
		return 0, 0, errors.New("the page element is not visible")
	}
	x, y := 0.0, 0.0
	for i := 0; i < 8; i += 2 {
		x += quad[i]
		y += quad[i+1]
	}
	return x / 4, y / 4, nil
}

func browserElementLabel(ref browserRef) string {
	if strings.TrimSpace(ref.Name) == "" {
		return "page element " + strconv.Itoa(ref.BackendNodeID)
	}
	return clipBrowser(ref.Name, 100)
}
