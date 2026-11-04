package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/webview2"
	"golang.org/x/sys/windows"
)

const (
	wvCallDevToolsProtocolMethod = 36 // ICoreWebView2::CallDevToolsProtocolMethod
	maxCDPReply                  = 4 << 20
)

type cdpReply struct {
	epoch uint64
	raw   json.RawMessage
	err   error
}

type cdpCompleted struct {
	refs  atomic.Int32
	ptr   uintptr
	epoch uint64
	once  sync.Once
	done  chan cdpReply
}

var cdpCallbacks = struct {
	sync.Mutex
	values map[uintptr]*webview2.ICoreWebView2CallDevToolsProtocolMethodCompletedHandler
}{values: make(map[uintptr]*webview2.ICoreWebView2CallDevToolsProtocolMethodCompletedHandler)}

func newCDPCompleted(epoch uint64, done chan cdpReply) (*cdpCompleted, *webview2.ICoreWebView2CallDevToolsProtocolMethodCompletedHandler) {
	h := &cdpCompleted{epoch: epoch, done: done}
	h.refs.Store(1) // caller's reference; WebView2 retains its own until completion
	com := webview2.NewICoreWebView2CallDevToolsProtocolMethodCompletedHandler(h)
	h.ptr = uintptr(unsafe.Pointer(com))
	cdpCallbacks.Lock()
	cdpCallbacks.values[h.ptr] = com
	cdpCallbacks.Unlock()
	return h, com
}

func (h *cdpCompleted) QueryInterface(_ uintptr, out uintptr) uintptr {
	if out == 0 {
		return uintptr(0x80004003) // E_POINTER
	}
	*(*uintptr)(unsafe.Pointer(out)) = h.ptr
	h.AddRef()
	return 0 // S_OK
}

func (h *cdpCompleted) AddRef() uintptr { return uintptr(h.refs.Add(1)) }

func (h *cdpCompleted) Release() uintptr {
	left := h.refs.Add(-1)
	if left <= 0 {
		cdpCallbacks.Lock()
		delete(cdpCallbacks.values, h.ptr)
		cdpCallbacks.Unlock()
		return 0
	}
	return uintptr(left)
}

func (h *cdpCompleted) CallDevToolsProtocolMethodCompleted(code uintptr, result string) uintptr {
	h.once.Do(func() {
		reply := cdpReply{epoch: h.epoch}
		if int32(code) < 0 {
			reply.err = fmt.Errorf("WebView2 CDP call failed (0x%08x)", uint32(code))
		} else if len(result) > maxCDPReply {
			reply.err = errors.New("browser response exceeded the size limit")
		} else {
			if strings.TrimSpace(result) == "" {
				result = "{}"
			}
			if !json.Valid([]byte(result)) {
				reply.err = errors.New("browser returned invalid protocol data")
			} else {
				reply.raw = append(json.RawMessage(nil), result...)
			}
		}
		h.done <- reply
	})
	return 0
}

// cdpCall schedules the COM call on Wails' UI thread, then waits off-thread for its async
// completion. Dependent calls are awaited by the caller; no DevTools port or poller is used.
func cdpCall(ctx context.Context, tabID int, expectedEpoch uint64, method string, params any) (json.RawMessage, uint64, error) {
	if len(method) < 3 || len(method) > 80 || !strings.Contains(method, ".") {
		return nil, 0, errors.New("invalid browser operation")
	}
	parameters, err := json.Marshal(params)
	if err != nil || len(parameters) > 32<<10 {
		return nil, 0, errors.New("browser operation arguments are invalid")
	}
	done := make(chan cdpReply, 1)
	runLater(func() {
		if ctx.Err() != nil {
			return
		}
		_, t := findTab(tabID)
		if t == nil || t.kind != kindWeb || t.wv == nil {
			done <- cdpReply{err: errors.New("the browser tab is no longer available")}
			return
		}
		epoch := t.browserEpoch.Load()
		if expectedEpoch != 0 && epoch != expectedEpoch {
			done <- cdpReply{epoch: epoch, err: errors.New("the page changed; take a fresh browser snapshot")}
			return
		}
		methodPtr, mErr := windows.UTF16PtrFromString(method)
		paramsPtr, pErr := windows.UTF16PtrFromString(string(parameters))
		if mErr != nil || pErr != nil {
			done <- cdpReply{epoch: epoch, err: errors.New("could not encode browser operation")}
			return
		}
		handler, com := newCDPCompleted(epoch, done)
		hr := comCall(t.wv, wvCallDevToolsProtocolMethod,
			uintptr(unsafe.Pointer(methodPtr)), uintptr(unsafe.Pointer(paramsPtr)), uintptr(unsafe.Pointer(com)))
		runtime.KeepAlive(methodPtr)
		runtime.KeepAlive(paramsPtr)
		runtime.KeepAlive(com)
		handler.Release() // release the caller reference; WebView2 owns the callback until done
		if err := hrErr("CallDevToolsProtocolMethod", hr); err != nil {
			handler.once.Do(func() { done <- cdpReply{epoch: epoch, err: err} })
		}
	})
	select {
	case reply := <-done:
		return reply.raw, reply.epoch, reply.err
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

func cdpCallJSON(ctx context.Context, tabID int, epoch uint64, method string, params any, dst any) error {
	raw, _, err := cdpCall(ctx, tabID, epoch, method, params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return errors.New("could not read browser response")
	}
	var protocolErr struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &protocolErr) == nil && protocolErr.Error != nil && protocolErr.Error.Message != "" {
		return errors.New(protocolErr.Error.Message)
	}
	return nil
}
