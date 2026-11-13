package main

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestDrawerClip(t *testing.T) {
	page := clipRect{100, 100, 900, 700}
	tests := []struct {
		name              string
		x, y, w, h, scale float64
		want              clipRect
		overlaps          bool
	}{
		{"inside", 200, 150, 300, 200, 1, clipRect{100, 50, 400, 250}, true},
		{"partial", 0, 50, 300, 200, 1, clipRect{0, 0, 200, 150}, true},
		{"outside", 1000, 100, 100, 100, 1, clipRect{}, false},
		{"scaled", 100, 100, 100, 100, 2, clipRect{100, 100, 300, 300}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := drawerClip(page, tc.x, tc.y, tc.w, tc.h, tc.scale)
			if ok != tc.overlaps || (ok && got != tc.want) {
				t.Fatalf("got %+v, %v; want %+v, %v", got, ok, tc.want, tc.overlaps)
			}
		})
	}
}

func TestBrowserHostRegionLeavesPageInteractive(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("Static")
	parent, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 800, 600, 0, 0, 0, 0)
	if parent == 0 {
		t.Fatalf("create parent window: %v", err)
	}
	defer pDestroyWindow.Call(parent)
	oldHWND, oldOpen, oldRect := hwnd, drawerOpen, drawerRect
	defer func() { hwnd, drawerOpen, drawerRect = oldHWND, oldOpen, oldRect }()
	hwnd = parent
	host := newBrowserHost()
	if host == 0 {
		t.Fatal("create browser host")
	}
	defer pDestroyWindow.Call(host)
	scale := float64(dpiFor(hwnd)) / 96
	drawerOpen = true
	drawerRect.x, drawerRect.y, drawerRect.w, drawerRect.h = 200/scale, 100/scale, 400/scale, 300/scale
	clipBrowserHost(host, clipRect{0, 0, 800, 600})
	region, _, _ := pCreateRectRgn.Call(0, 0, 0, 0)
	if region == 0 {
		t.Fatal("create region")
	}
	defer pDeleteObject.Call(region)
	getRegion := user32.NewProc("GetWindowRgn")
	if result, _, _ := getRegion.Call(host, region); result == 0 {
		t.Fatal("get browser region")
	}
	inside := gdi32.NewProc("PtInRegion")
	outsidePanel, _, _ := inside.Call(region, 50, 50)
	underPanel, _, _ := inside.Call(region, 250, 150)
	if outsidePanel == 0 || underPanel != 0 {
		t.Fatalf("unexpected region: outside=%d under panel=%d", outsidePanel, underPanel)
	}
	drawerRect.radius = 25 / scale
	clipBrowserHost(host, clipRect{0, 0, 800, 600})
	if result, _, _ := getRegion.Call(host, region); result == 0 {
		t.Fatal("get rounded browser region")
	}
	corner, _, _ := inside.Call(region, 200, 100)
	center, _, _ := inside.Call(region, 250, 150)
	if corner == 0 || center != 0 {
		t.Fatalf("unexpected rounded region: corner=%d center=%d", corner, center)
	}
	drawerOpen = false
	clipBrowserHost(host, clipRect{0, 0, 800, 600})
	if result, _, _ := getRegion.Call(host, region); result != 0 {
		t.Fatal("browser region was not cleared")
	}
}
