package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Minimal raw COM helpers.
//
// go-webview2 v1.0.23's generated bindings (pkg/webview2) are not safe to use for
// anything that sets a BOOL: they pass a pointer where the API takes the value, and some
// derived-interface vtables are truncated. Its hand-written pkg/edge is correct for what
// it covers. Everything else goes through these helpers, with vtable slot numbers taken
// from the WebView2 IDL order and verified against the generated vtable layout.

func vtblFn(obj unsafe.Pointer, slot uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	return *(*uintptr)(unsafe.Add(vtbl, slot*unsafe.Sizeof(uintptr(0))))
}

func comCall(obj unsafe.Pointer, slot uintptr, args ...uintptr) uintptr {
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(obj))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(vtblFn(obj, slot), all...)
	return r
}

func hrErr(what string, hr uintptr) error {
	if int32(hr) >= 0 {
		return nil
	}
	return fmt.Errorf("%s failed (hr=0x%08x)", what, uint32(hr))
}

var pCoTaskMemFree = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemFree")

// comGetString calls a getter of the form HRESULT get_X(LPWSTR* value) and frees the result.
func comGetString(obj unsafe.Pointer, slot uintptr) string {
	var p *uint16
	if hr := comCall(obj, slot, uintptr(unsafe.Pointer(&p))); int32(hr) < 0 || p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return s
}

// comGetBool calls HRESULT get_X(BOOL* value).
func comGetBool(obj unsafe.Pointer, slot uintptr) bool {
	var v int32
	if hr := comCall(obj, slot, uintptr(unsafe.Pointer(&v))); int32(hr) < 0 {
		return false
	}
	return v != 0
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// ── IStream (for CapturePreview) ───────────────────────────────────────────

var pSHCreateMemStream = windows.NewLazySystemDLL("shlwapi.dll").NewProc("SHCreateMemStream")

const (
	streamRead = 3
	streamSeek = 5

	streamSeekSet = 0
	streamSeekEnd = 2
)

// newMemStream returns an empty in-memory IStream. The caller releases it (slot 2).
func newMemStream() unsafe.Pointer {
	p, _, _ := pSHCreateMemStream.Call(0, 0)
	return unsafe.Pointer(p)
}

// readAll returns the whole content of a seekable IStream.
func readAll(stream unsafe.Pointer) ([]byte, error) {
	var size uint64
	if hr := comCall(stream, streamSeek, 0, streamSeekEnd, uintptr(unsafe.Pointer(&size))); hrErr("IStream.Seek(end)", hr) != nil {
		return nil, hrErr("IStream.Seek(end)", hr)
	}
	if hr := comCall(stream, streamSeek, 0, streamSeekSet, 0); hrErr("IStream.Seek(set)", hr) != nil {
		return nil, hrErr("IStream.Seek(set)", hr)
	}
	if size == 0 || size > 64<<20 {
		return nil, fmt.Errorf("unexpected stream size %d", size)
	}
	buf := make([]byte, size)
	var n uint32
	if hr := comCall(stream, streamRead, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&n))); hrErr("IStream.Read", hr) != nil {
		return nil, hrErr("IStream.Read", hr)
	}
	return buf[:n], nil
}

// ── Settings9 ─────────────────────────────────────────────────────────────

// IID_ICoreWebView2Settings9
var iidSettings9 = windows.GUID{
	Data1: 0x0528a73b, Data2: 0xe92d, Data3: 0x49f4,
	Data4: [8]byte{0x92, 0x7a, 0xe5, 0x47, 0xdd, 0xda, 0xa3, 0x7d},
}

// Vtable slots of ICoreWebView2Settings9, counted from the IDL inheritance chain:
// IUnknown (3) + Settings (18) + Settings2 (2) + Settings3 (2) + Settings4 (4) +
// Settings5 (2) + Settings6 (2) + Settings7 (2) + Settings8 (2) = 37.
const (
	slotGetNonClientRegion = 37
	slotPutNonClientRegion = 38
)

// enableNonClientRegions turns on CSS app-region support and reads the value back.
// The read-back verifies that the slot numbering above is right.
func enableNonClientRegions(settings unsafe.Pointer) (bool, error) {
	var s9 unsafe.Pointer
	if hr := comCall(settings, 0, uintptr(unsafe.Pointer(&iidSettings9)), uintptr(unsafe.Pointer(&s9))); hr != 0 || s9 == nil {
		return false, fmt.Errorf("ICoreWebView2Settings9 not available (hr=0x%x)", hr)
	}
	defer comCall(s9, 2) // Release

	if hr := comCall(s9, slotPutNonClientRegion, 1); hr != 0 {
		return false, fmt.Errorf("put_IsNonClientRegionSupportEnabled failed (hr=0x%x)", hr)
	}
	var got int32
	if hr := comCall(s9, slotGetNonClientRegion, uintptr(unsafe.Pointer(&got))); hr != 0 {
		return false, fmt.Errorf("get_IsNonClientRegionSupportEnabled failed (hr=0x%x)", hr)
	}
	return got != 0, nil
}

// ── ICoreWebView2 slots (IDL order) ───────────────────────────────────────

const (
	wvGetSource                 = 4
	wvAddNavigationStarting     = 7
	wvAddSourceChanged          = 11
	wvAddHistoryChanged         = 13
	wvExecuteScript             = 29
	wvCapturePreview            = 30
	wvReload                    = 31
	wvGetCanGoBack              = 38
	wvGetCanGoForward           = 39
	wvGoBack                    = 40
	wvGoForward                 = 41
	wvStop                      = 43
	wvAddNewWindowRequested     = 44
	wvAddDocumentTitleChanged   = 46
	wvGetDocumentTitle          = 48
	ctrlClose                   = 24 // ICoreWebView2Controller::Close
	navStartingGetURI           = 3  // ICoreWebView2NavigationStartingEventArgs::get_Uri
	navCompletedGetIsSuccess    = 3  // ICoreWebView2NavigationCompletedEventArgs::get_IsSuccess
	newWindowGetURI             = 3  // ICoreWebView2NewWindowRequestedEventArgs::get_Uri
	newWindowPutNewWindow       = 4
	newWindowPutHandled         = 6
	newWindowGetDeferral        = 9
	deferralComplete            = 3
	wvAddWindowCloseRequested   = 59
	newWindowGetIsUserInitiated = 8
)

// ── A reusable event-handler COM object ───────────────────────────────────
//
// Every WebView2 event handler we need has the shape Invoke(sender, args) -> HRESULT, and
// the completion handlers take a single HRESULT; extra register arguments are ignored on
// amd64, so one class serves all of them.

type eventHandler struct {
	vtbl *eventHandlerVtbl
	fn   func(sender, args unsafe.Pointer)
}

type eventHandlerVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	invoke         uintptr
}

var (
	handlerVtbl *eventHandlerVtbl
	// Handlers must stay reachable for as long as WebView2 may call them.
	liveHandlers []*eventHandler
)

func init() {
	handlerVtbl = &eventHandlerVtbl{
		queryInterface: syscall.NewCallback(func(this, riid, ppv uintptr) uintptr {
			*(*uintptr)(unsafe.Pointer(ppv)) = this
			return 0 // S_OK
		}),
		addRef:  syscall.NewCallback(func(this uintptr) uintptr { return 1 }),
		release: syscall.NewCallback(func(this uintptr) uintptr { return 1 }),
		invoke: syscall.NewCallback(func(this, sender, args uintptr) uintptr {
			h := (*eventHandler)(unsafe.Pointer(this))
			h.fn(unsafe.Pointer(sender), unsafe.Pointer(args))
			return 0
		}),
	}
}

func newHandler(fn func(sender, args unsafe.Pointer)) *eventHandler {
	h := &eventHandler{vtbl: handlerVtbl, fn: fn}
	liveHandlers = append(liveHandlers, h)
	return h
}

// addEvent registers fn for an event whose add_X method is at the given ICoreWebView2 slot.
func addEvent(webview unsafe.Pointer, slot uintptr, what string, fn func(sender, args unsafe.Pointer)) error {
	h := newHandler(fn)
	var token int64
	hr := comCall(webview, slot, uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&token)))
	return hrErr("add "+what, hr)
}
