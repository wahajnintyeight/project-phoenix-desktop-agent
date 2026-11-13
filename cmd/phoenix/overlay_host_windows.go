package main

import (
	"math"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32               = windows.NewLazySystemDLL("gdi32.dll")
	pCreateWindowEx     = user32.NewProc("CreateWindowExW")
	pDestroyWindow      = user32.NewProc("DestroyWindow")
	pShowWindow         = user32.NewProc("ShowWindow")
	pSetWindowRgn       = user32.NewProc("SetWindowRgn")
	pCreateRectRgn      = gdi32.NewProc("CreateRectRgn")
	pCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	pCombineRgn         = gdi32.NewProc("CombineRgn")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
)

// Each browser gets a host window so its visible region can exclude the agent panel.
func newBrowserHost() uintptr {
	class, _ := windows.UTF16PtrFromString("Static")
	h, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0, 0x44000000, 0, 0, 1, 1, hwnd, 0, 0, 0)
	if h == 0 {
		logf("create browser host: %v", err)
	}
	return h
}

type clipRect struct{ left, top, right, bottom int32 }

func setDrawerRect(fields []string) {
	if len(fields) < 4 {
		return
	}
	var v [4]float64
	for i := range v {
		n, err := strconv.ParseFloat(fields[i], 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < -20000 || n > 20000 || (i >= 2 && n < 0) {
			return
		}
		v[i] = n
	}
	radius := 0.0
	if len(fields) > 4 {
		n, err := strconv.ParseFloat(fields[4], 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 10000 {
			return
		}
		radius = n
	}
	if drawerRect.x == v[0] && drawerRect.y == v[1] && drawerRect.w == v[2] && drawerRect.h == v[3] && drawerRect.radius == radius {
		return
	}
	drawerRect.x, drawerRect.y, drawerRect.w, drawerRect.h = v[0], v[1], v[2], v[3]
	drawerRect.radius = radius
	if t := activeTab(); t != nil {
		t.place()
	}
}

func drawerClip(page clipRect, x, y, w, h, scale float64) (clipRect, bool) {
	left := int32(math.Round(x*scale)) - page.left
	top := int32(math.Round(y*scale)) - page.top
	right := int32(math.Round((x+w)*scale)) - page.left
	bottom := int32(math.Round((y+h)*scale)) - page.top
	r := clipRect{max(0, left), max(0, top), min(page.right-page.left, right), min(page.bottom-page.top, bottom)}
	return r, r.right > r.left && r.bottom > r.top
}

func clipBrowserHost(host uintptr, page clipRect) {
	if !drawerOpen || drawerRect.w <= 0 || drawerRect.h <= 0 {
		pSetWindowRgn.Call(host, 0, 1)
		return
	}
	r, overlaps := drawerClip(page, drawerRect.x, drawerRect.y, drawerRect.w, drawerRect.h, float64(dpiFor(hwnd))/96)
	if !overlaps {
		pSetWindowRgn.Call(host, 0, 1)
		return
	}
	full, _, _ := pCreateRectRgn.Call(0, 0, uintptr(page.right-page.left), uintptr(page.bottom-page.top))
	cut, _, _ := pCreateRectRgn.Call(uintptr(r.left), uintptr(r.top), uintptr(r.right), uintptr(r.bottom))
	if drawerRect.radius > 0 {
		if cut != 0 {
			pDeleteObject.Call(cut)
		}
		scale := float64(dpiFor(hwnd)) / 96
		// Leave room for WebView's antialiased pill edge outside the native region.
		const margin int32 = 2
		left := int32(math.Round(drawerRect.x*scale)) - page.left - margin
		top := int32(math.Round(drawerRect.y*scale)) - page.top - margin
		right := int32(math.Round((drawerRect.x+drawerRect.w)*scale)) - page.left + margin
		bottom := int32(math.Round((drawerRect.y+drawerRect.h)*scale)) - page.top + margin
		diameter := uintptr(int32(math.Round(drawerRect.radius*2*scale)) + 2*margin)
		cut, _, _ = pCreateRoundRectRgn.Call(uintptr(left), uintptr(top), uintptr(right), uintptr(bottom), diameter, diameter)
	}
	if full == 0 || cut == 0 {
		if full != 0 {
			pDeleteObject.Call(full)
		}
		if cut != 0 {
			pDeleteObject.Call(cut)
		}
		return
	}
	result, _, _ := pCombineRgn.Call(full, full, cut, 4) // RGN_DIFF
	pDeleteObject.Call(cut)
	if result == 0 {
		pDeleteObject.Call(full)
		return
	}
	if ok, _, _ := pSetWindowRgn.Call(host, full, 1); ok == 0 {
		pDeleteObject.Call(full)
	}
}
