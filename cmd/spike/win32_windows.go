package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")

	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pDestroyWindow                 = user32.NewProc("DestroyWindow")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pIsZoomed                      = user32.NewProc("IsZoomed")
	pGetWindowRect                 = user32.NewProc("GetWindowRect")
	pSetWindowPos                  = user32.NewProc("SetWindowPos")
	pGetWindow                     = user32.NewProc("GetWindow")
	pLoadCursorW                   = user32.NewProc("LoadCursorW")
	pGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	pGetSystemMetricsForDpi        = user32.NewProc("GetSystemMetricsForDpi")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pGetModuleHandleW              = kernel32.NewProc("GetModuleHandleW")
	pDwmExtendFrameIntoClientArea  = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	pCreateSolidBrush              = gdi32.NewProc("CreateSolidBrush")
)

const (
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmGetMinMaxInfo = 0x0024
	wmNCCalcSize    = 0x0083
	wmNCHitTest     = 0x0084
	wmDPIChanged    = 0x02E0

	wsOverlappedWindow = 0x00CF0000
	wsClipChildren     = 0x02000000
	wsClipSiblings     = 0x04000000

	swHide     = 0
	swMaximize = 3
	swShow     = 5
	swMinimize = 6
	swRestore  = 9

	sizeMaximized = 2

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	htClient      = 1
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17

	smCxFrame        = 32
	smCyFrame        = 33
	smCxPaddedBorder = 92

	gwChild         = 5
	hwndBottom      = 1
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	cwUseDefault = 0x80000000
	idcArrow     = 32512
)

type point struct{ x, y int32 }
type rect struct{ left, top, right, bottom int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	_       uint32
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type minMaxInfo struct {
	ptReserved     point
	ptMaxSize      point
	ptMaxPosition  point
	ptMinTrackSize point
	ptMaxTrackSize point
}

type ncCalcSizeParams struct {
	rgrc  [3]rect
	lppos uintptr
}

type margins struct{ left, right, top, bottom int32 }

func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func isZoomed(hwnd uintptr) bool {
	r, _, _ := pIsZoomed.Call(hwnd)
	return r != 0
}

func dpiFor(hwnd uintptr) uint32 {
	r, _, _ := pGetDpiForWindow.Call(hwnd)
	if r == 0 {
		return 96
	}
	return uint32(r)
}

func metric(index int, dpi uint32) int32 {
	r, _, _ := pGetSystemMetricsForDpi.Call(uintptr(index), uintptr(dpi))
	return int32(r)
}

func loword(v uintptr) int32 { return int32(int16(v & 0xFFFF)) }
func hiword(v uintptr) int32 { return int32(int16((v >> 16) & 0xFFFF)) }

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}
