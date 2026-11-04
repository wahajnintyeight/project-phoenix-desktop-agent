//go:build windows

package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procPrintWindow        = user32.NewProc("PrintWindow")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	captureSlot            = make(chan struct{}, 1)
)

type windowRect struct{ Left, Top, Right, Bottom int32 }
type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}
type boundedPNG struct{ bytes.Buffer }

func (b *boundedPNG) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxScreen {
		return 0, errors.New("screenshot is too large")
	}
	return b.Buffer.Write(p)
}

func captureNativeWindow(ctx context.Context, hwnd uintptr) ([]byte, error) {
	select {
	case captureSlot <- struct{}{}:
	default:
		return nil, errors.New("a previous screenshot is still running")
	}
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() { defer func() { <-captureSlot }(); b, e := captureWindow(hwnd); done <- result{b, e} }()
	timeout := time.NewTimer(8 * time.Second)
	defer timeout.Stop()
	select {
	case r := <-done:
		return r.data, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timeout.C:
		return nil, errors.New("the application did not respond to screenshot capture")
	}
}

func captureWindow(hwnd uintptr) ([]byte, error) {
	var rect windowRect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if ok == 0 {
		return nil, errors.New("the window is no longer available")
	}
	width, height := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	if width <= 0 || height <= 0 || width > 4096 || height > 4096 || int64(width)*int64(height) > 8_000_000 {
		return nil, errors.New("the window is too large or has no visible area")
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, errors.New("could not get a display context")
	}
	defer procReleaseDC.Call(0, screen)
	dc, _, _ := procCreateCompatibleDC.Call(screen)
	if dc == 0 {
		return nil, errors.New("could not create a screenshot context")
	}
	defer procDeleteDC.Call(dc)
	info := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	bitmap, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return nil, errors.New("could not allocate screenshot pixels")
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(dc, bitmap)
	if old == 0 || old == ^uintptr(0) {
		return nil, errors.New("could not select screenshot bitmap")
	}
	defer procSelectObject.Call(dc, old)
	ok, _, _ = procPrintWindow.Call(hwnd, dc, 2)
	if ok == 0 {
		return nil, errors.New("this application does not support window capture")
	}
	pixels := unsafe.Slice((*byte)(bits), width*height*4)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(pixels); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], 255
	}
	var out boundedPNG
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("could not encode screenshot: %w", err)
	}
	return out.Bytes(), nil
}
