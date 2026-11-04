//go:build windows

package desktop

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unsafe"

	uia "github.com/auuunya/go-element"
)

var (
	procSetForeground = user32.NewProc("SetForegroundWindow")
	procSetCursor     = user32.NewProc("SetCursorPos")
	procMouseEvent    = user32.NewProc("mouse_event")
	procSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	mouseLeftDown = 0x0002
	mouseLeftUp   = 0x0004
	mouseWheel    = 0x0800
	mouseHWheel   = 0x1000
)

type screenPoint struct{ x, y int32 }

func (s *automationState) point(el *uia.IUIAutomationElement) (screenPoint, error) {
	if el.Get_CurrentIsOffscreen() != 0 {
		return screenPoint{}, errors.New("the control is off screen; scroll it into view first")
	}
	rect := el.Get_CurrentBoundingRectangle()
	if rect == nil || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return screenPoint{}, errors.New("STALE_REF: the control has no visible bounds; take a fresh snapshot")
	}
	p := screenPoint{x: rect.Left + (rect.Right-rect.Left)/2, y: rect.Top + (rect.Bottom-rect.Top)/2}
	if !onVirtualScreen(p) {
		return screenPoint{}, errors.New("the control is outside the visible desktop")
	}
	return p, nil
}
func onVirtualScreen(p screenPoint) bool {
	x, _, _ := procSystemMetrics.Call(76)
	y, _, _ := procSystemMetrics.Call(77)
	w, _, _ := procSystemMetrics.Call(78)
	h, _, _ := procSystemMetrics.Call(79)
	left, top := int64(int32(x)), int64(int32(y))
	return int64(p.x) >= left && int64(p.y) >= top && int64(p.x) < left+int64(w) && int64(p.y) < top+int64(h)
}
func foreground(hwnd uintptr) error {
	if hwnd == 0 {
		return errors.New("no target window")
	}
	active, _, _ := procGetForegroundWindow.Call()
	if active == hwnd {
		return nil
	}
	procSetForeground.Call(hwnd)
	active, _, _ = procGetForegroundWindow.Call()
	if active != hwnd {
		return errors.New("could not focus the target window; bring it to the front and try again")
	}
	return nil
}
func stillForeground(hwnd uintptr) error {
	active, _, _ := procGetForegroundWindow.Call()
	if active != hwnd {
		return errors.New("focus changed; inspect the target before retrying")
	}
	return nil
}

func moveCursor(p screenPoint) error {
	ok, _, _ := procSetCursor.Call(uintptr(uint32(p.x)), uintptr(uint32(p.y)))
	if ok == 0 {
		return errors.New("could not move the pointer")
	}
	return nil
}
func (s *automationState) click(ctx context.Context, el *uia.IUIAutomationElement) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	element := uia.NewElement(el)
	if pattern, err := element.GetInvokePattern(); err == nil && pattern != nil {
		defer releaseCOM(unsafe.Pointer(pattern))
		return pattern.Invoke()
	}
	if pattern, err := element.GetTogglePattern(); err == nil && pattern != nil {
		defer releaseCOM(unsafe.Pointer(pattern))
		return pattern.Toggle()
	}
	if pattern, err := element.GetSelectionItemPattern(); err == nil && pattern != nil {
		defer releaseCOM(unsafe.Pointer(pattern))
		return pattern.Select()
	}
	point, err := s.point(el)
	if err != nil {
		return err
	}
	if err = foreground(s.hwnd); err != nil {
		return err
	}
	if err = moveCursor(point); err != nil {
		return err
	}
	if err = stillForeground(s.hwnd); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	procMouseEvent.Call(mouseLeftDown, 0, 0, 0, 0)
	procMouseEvent.Call(mouseLeftUp, 0, 0, 0, 0)
	return nil
}
func nativeSetValue(el *uia.IUIAutomationElement, value string) error {
	pattern, err := uia.NewElement(el).GetValuePattern()
	if err != nil || pattern == nil {
		return errors.New("this control does not support setting text")
	}
	defer releaseCOM(unsafe.Pointer(pattern))
	return pattern.SetValue(value)
}

func (s *automationState) scroll(ctx context.Context, el *uia.IUIAutomationElement, direction, amount string) error {
	n, err := strconv.Atoi(amount)
	if err != nil || n < 1 || n > 50 {
		return errors.New("invalid scroll amount")
	}
	p, err := s.point(el)
	if err != nil {
		return err
	}
	if err = foreground(s.hwnd); err != nil {
		return err
	}
	if err = moveCursor(p); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = stillForeground(s.hwnd); err != nil {
		return err
	}
	flag, delta := uintptr(mouseWheel), int32(n*120)
	switch direction {
	case "up":
	case "down":
		delta = -delta
	case "left":
		flag = mouseHWheel
		delta = -delta
	case "right":
		flag = mouseHWheel
	default:
		return errors.New("invalid scroll direction")
	}
	procMouseEvent.Call(flag, 0, 0, uintptr(uint32(delta)), 0)
	return nil
}
func parsePoint(raw string) (screenPoint, error) {
	a, b, ok := strings.Cut(raw, ",")
	if !ok {
		return screenPoint{}, errors.New("invalid point")
	}
	x, e1 := strconv.ParseInt(a, 10, 32)
	y, e2 := strconv.ParseInt(b, 10, 32)
	p := screenPoint{int32(x), int32(y)}
	if e1 != nil || e2 != nil || !onVirtualScreen(p) {
		return screenPoint{}, errors.New("point is outside the visible desktop")
	}
	return p, nil
}
func (s *automationState) endpoint(args []string, refFlag, xyFlag string) (screenPoint, error) {
	ref := option(args, refFlag)
	if ref != "" {
		el, err := s.get(ref)
		if err != nil {
			return screenPoint{}, err
		}
		return s.point(el)
	}
	for _, arg := range args {
		if raw, ok := strings.CutPrefix(arg, xyFlag+"="); ok {
			return parsePoint(raw)
		}
	}
	return screenPoint{}, errors.New("missing drag endpoint")
}
func (s *automationState) drag(ctx context.Context, args []string) error {
	from, err := s.endpoint(args, "--from", "--from-xy")
	if err != nil {
		return err
	}
	to, err := s.endpoint(args, "--to", "--to-xy")
	if err != nil {
		return err
	}
	if s.hwnd != 0 && (option(args, "--from") != "" || option(args, "--to") != "") {
		if err = foreground(s.hwnd); err != nil {
			return err
		}
	}
	if err = moveCursor(from); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	procMouseEvent.Call(mouseLeftDown, 0, 0, 0, 0)
	defer procMouseEvent.Call(mouseLeftUp, 0, 0, 0, 0)
	for step := int32(1); step <= 12; step++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		if s.hwnd != 0 && (option(args, "--from") != "" || option(args, "--to") != "") {
			if err = stillForeground(s.hwnd); err != nil {
				return err
			}
		}
		p := screenPoint{from.x + (to.x-from.x)*step/12, from.y + (to.y-from.y)*step/12}
		if err = moveCursor(p); err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}
func nativePress(ctx context.Context, app, chord string) error {
	keys, err := parseKeys(chord)
	if err != nil {
		return err
	}
	list, err := nativeWindows(ctx)
	if err != nil {
		return err
	}
	var hwnd uintptr
	for _, w := range list {
		if strings.EqualFold(w.App, app) && w.Focused {
			var value uint64
			value, err = strconv.ParseUint(strings.TrimPrefix(w.ID, "w-"), 10, 64)
			hwnd = uintptr(value)
			break
		}
	}
	if hwnd == 0 {
		hwnd, err = selectNativeWindow(list, app, "")
		if err != nil {
			return err
		}
	}
	if err = foreground(hwnd); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return sendVirtualKeys(keys)
}
func parseKeys(chord string) ([]byte, error) {
	parts := strings.Split(strings.ToLower(chord), "+")
	if len(parts) == 0 || len(parts) > 4 {
		return nil, errors.New("unsupported key combination")
	}
	modifiers := map[string]byte{"ctrl": 0x11, "shift": 0x10, "alt": 0x12, "win": 0x5b}
	named := map[string]byte{"enter": 0x0d, "escape": 0x1b, "esc": 0x1b, "tab": 0x09, "space": 0x20, "backspace": 0x08, "delete": 0x2e, "home": 0x24, "end": 0x23, "pageup": 0x21, "pagedown": 0x22, "up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27}
	keys := make([]byte, 0, len(parts))
	seen := map[byte]bool{}
	for i, part := range parts {
		var key byte
		if i < len(parts)-1 {
			key = modifiers[part]
		} else if len(part) == 1 && ((part[0] >= 'a' && part[0] <= 'z') || (part[0] >= '0' && part[0] <= '9')) {
			key = byte(strings.ToUpper(part)[0])
		} else {
			key = named[part]
		}
		if key == 0 || seen[key] {
			return nil, fmt.Errorf("unsupported key combination %q", chord)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys, nil
}
func nativeClickAt(ctx context.Context, app, id, rawX, rawY string) error {
	x, e1 := strconv.ParseInt(rawX, 10, 32)
	y, e2 := strconv.ParseInt(rawY, 10, 32)
	if e1 != nil || e2 != nil || x < 0 || y < 0 {
		return errors.New("invalid window-relative point")
	}
	list, err := nativeWindows(ctx)
	if err != nil {
		return err
	}
	hwnd, err := selectNativeWindow(list, app, id)
	if err != nil {
		return err
	}
	var rect windowRect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if ok == 0 || x >= int64(rect.Right-rect.Left) || y >= int64(rect.Bottom-rect.Top) {
		return errors.New("the point is outside the target window")
	}
	p := screenPoint{x: rect.Left + int32(x), y: rect.Top + int32(y)}
	if !onVirtualScreen(p) {
		return errors.New("the point is outside the visible desktop")
	}
	if err = foreground(hwnd); err != nil {
		return err
	}
	if err = moveCursor(p); err != nil {
		return err
	}
	if err = stillForeground(hwnd); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	procMouseEvent.Call(mouseLeftDown, 0, 0, 0, 0)
	procMouseEvent.Call(mouseLeftUp, 0, 0, 0, 0)
	return nil
}
