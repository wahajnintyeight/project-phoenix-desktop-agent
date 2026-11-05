//go:build windows

package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	uia "github.com/auuunya/go-element"
	"golang.org/x/sys/windows"
)

var (
	ole32          = windows.NewLazySystemDLL("ole32.dll")
	coInitializeEx = ole32.NewProc("CoInitializeEx")
	coUninitialize = ole32.NewProc("CoUninitialize")
)

type nativeRequest struct {
	ctx   context.Context
	args  []string
	reply chan nativeResponse
}
type nativeResponse struct {
	env Envelope
	err error
}
type automationDriver struct {
	once     sync.Once
	requests chan nativeRequest
}
type automationState struct {
	automation *uia.IUIAutomation
	refs       map[string]*uia.IUIAutomationElement
	hwnd       uintptr
}

func newNativeDriver() nativeDriver { return &automationDriver{requests: make(chan nativeRequest)} }

func warmDriver(d nativeDriver) {
	if a, ok := d.(*automationDriver); ok {
		a.once.Do(func() { go a.worker() })
	}
}

func (d *automationDriver) run(ctx context.Context, args []string) (Envelope, bool, error) {
	if len(args) == 0 {
		return Envelope{}, false, nil
	}
	switch args[0] {
	case "snapshot", "click", "click-at", "scroll", "drag", "type", "set-value", "press":
	default:
		return Envelope{}, false, nil
	}
	d.once.Do(func() { go d.worker() })
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req := nativeRequest{ctx: ctx, args: append([]string(nil), args...), reply: make(chan nativeResponse, 1)}
	select {
	case d.requests <- req:
	case <-ctx.Done():
		return Envelope{}, true, ctx.Err()
	}
	select {
	case result := <-req.reply:
		return result.env, true, result.err
	case <-ctx.Done():
		return Envelope{}, true, errors.New("desktop automation timed out; the target app may be unresponsive")
	}
}
func (d *automationDriver) worker() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := coInitializeEx.Call(0, 0)
	var initErr error
	if int32(hr) < 0 {
		initErr = fmt.Errorf("could not initialize Windows automation (0x%x)", uint32(hr))
	} else {
		defer coUninitialize.Call()
	}
	state := automationState{refs: map[string]*uia.IUIAutomationElement{}}
	if initErr == nil {
		ptr, err := uia.CreateInstance(uia.CLSID_CUIAutomation, uia.IID_IUIAutomation, uia.CLSCTX_INPROC_SERVER)
		if err != nil {
			initErr = err
		} else {
			state.automation = uia.NewIUIAutomation(uia.NewIUnKnown(ptr))
		}
	}
	for req := range d.requests {
		if req.ctx.Err() != nil {
			req.reply <- nativeResponse{err: req.ctx.Err()}
			continue
		}
		if initErr != nil {
			req.reply <- nativeResponse{err: fmt.Errorf("Windows automation unavailable: %w", initErr)}
			continue
		}
		value, err := state.execute(req.ctx, req.args)
		if err != nil {
			req.reply <- nativeResponse{err: err}
			continue
		}
		env, err := nativeEnvelope(req.args[0], value)
		req.reply <- nativeResponse{env: env, err: err}
	}
}
func releaseCOM(p unsafe.Pointer) {
	if p != nil {
		uia.NewIUnKnown(p).Release()
	}
}

func (s *automationState) clear() {
	for _, el := range s.refs {
		releaseCOM(unsafe.Pointer(el))
	}
	s.refs = map[string]*uia.IUIAutomationElement{}
	s.hwnd = 0
}

func (s *automationState) snapshot(ctx context.Context, app, id string) (any, error) {
	windowsList, err := nativeWindows(ctx)
	if err != nil {
		return nil, err
	}
	hwnd, err := selectNativeWindow(windowsList, app, id)
	if err != nil {
		return nil, err
	}
	root, err := s.automation.ElementFromHandle(hwnd)
	if err != nil || root == nil {
		return nil, errors.New("could not access that window's controls")
	}
	defer releaseCOM(unsafe.Pointer(root))
	condition := s.automation.CreateTrueCondition()
	if condition == nil {
		return nil, errors.New("could not initialize control search")
	}
	defer releaseCOM(unsafe.Pointer(condition))
	var nonce [5]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	snapshotID := "s" + hex.EncodeToString(nonce[:])
	s.clear()
	s.hwnd = hwnd
	flat := make([]node, 0, 64)
	visited := 0
	var walk func(*uia.IUIAutomationElement, int) error
	walk = func(parent *uia.IUIAutomationElement, depth int) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if depth >= 24 || len(flat) >= maxOutline || visited >= 800 {
			return nil
		}
		children, err := parent.FindAll(condition)
		if err != nil {
			return nil
		}
		if children == nil {
			return nil
		}
		defer children.Release()
		count := int(children.Get_Length())
		if count < 0 {
			return nil
		}
		for i := 0; i < count && len(flat) < maxOutline && visited < 800; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			visited++
			el, err := children.GetElement(int32(i))
			if err != nil || el == nil {
				continue
			}
			name, _ := el.Get_CurrentName()
			role, _ := el.Get_CurrentLocalizedControlType()
			name = clipText(name, maxLabelLen)
			role = clipText(role, 40)
			if role == "" {
				role = "control"
			}
			ref := fmt.Sprintf("@%s:e%d", snapshotID, len(flat)+1)
			states := []string{}
			password := el.Get_CurrentIsPassword() != 0
			value := ""
			if !password && el.Get_CurrentControlType() == uia.UIA_EditControlTypeId {
				pattern, err := uia.NewElement(el).GetValuePattern()
				if err == nil && pattern != nil {
					value, _ = pattern.Get_CurrentValue()
					releaseCOM(unsafe.Pointer(pattern))
					value = clipText(value, maxLabelLen)
				}
			}
			if el.Get_CurrentIsEnabled() == 0 {
				states = append(states, "disabled")
			}
			if el.Get_CurrentIsOffscreen() != 0 {
				states = append(states, "offscreen")
			}
			if password {
				states = append(states, "password")
			}
			flat = append(flat, node{Ref: ref, Role: role, Name: name, Value: value, States: states})
			s.refs[ref] = el
			if err := walk(el, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, 0); err != nil {
		s.clear()
		return nil, err
	}
	return map[string]any{"snapshot_id": snapshotID, "tree": node{Role: "window", Children: flat}}, nil
}
func option(args []string, key string) string {
	for i := 1; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}
func trailing(args []string) []string {
	for i, a := range args {
		if a == "--" {
			return args[i+1:]
		}
	}
	return nil
}
func (s *automationState) get(ref string) (*uia.IUIAutomationElement, error) {
	el := s.refs[ref]
	if el == nil {
		return nil, errors.New("STALE_REF: take a fresh desktop_snapshot")
	}
	if el.Get_CurrentIsEnabled() == 0 {
		return nil, errors.New("the control is no longer enabled or available")
	}
	return el, nil
}
func (s *automationState) execute(ctx context.Context, args []string) (any, error) {
	switch args[0] {
	case "snapshot":
		return s.snapshot(ctx, option(args, "--app"), option(args, "--window-id"))
	case "click":
		tail := trailing(args)
		if len(tail) != 1 {
			return nil, errors.New("missing control reference")
		}
		el, err := s.get(tail[0])
		if err != nil {
			return nil, err
		}
		return nil, s.click(ctx, el)
	case "click-at":
		return nil, nativeClickAt(ctx, option(args, "--app"), option(args, "--window-id"), option(args, "--x"), option(args, "--y"))
	case "set-value", "type":
		tail := trailing(args)
		if len(tail) != 2 {
			return nil, errors.New("missing control reference or text")
		}
		el, err := s.get(tail[0])
		if err != nil {
			return nil, err
		}
		if args[0] == "type" {
			return nil, s.typeText(ctx, el, tail[1])
		}
		return nil, nativeSetValue(el, tail[1])
	case "scroll":
		tail := trailing(args)
		if len(tail) != 1 {
			return nil, errors.New("missing control reference")
		}
		el, err := s.get(tail[0])
		if err != nil {
			return nil, err
		}
		return nil, s.scroll(ctx, el, option(args, "--direction"), option(args, "--amount"))
	case "drag":
		return nil, s.drag(ctx, args)
	case "press":
		tail := trailing(args)
		if len(tail) != 1 {
			return nil, errors.New("missing key combination")
		}
		return nil, nativePress(ctx, option(args, "--app"), tail[0])
	}
	return nil, errors.New("unsupported desktop action: " + strings.TrimSpace(args[0]))
}
