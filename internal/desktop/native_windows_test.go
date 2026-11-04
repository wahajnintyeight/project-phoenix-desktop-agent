//go:build windows

package desktop

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/windows"
)

func TestNativeWindowSelection(t *testing.T) {
	list := []nativeWindow{{App: "Editor.exe", ID: "w-101"}, {App: "editor.exe", ID: "w-202"}, {App: "other.exe", ID: "w-303", Minimized: true}}
	if _, err := selectNativeWindow(list, "editor.exe", ""); err == nil {
		t.Fatal("ambiguous app accepted")
	}
	if got, err := selectNativeWindow(list, "editor.exe", "w-202"); err != nil || got != 202 {
		t.Fatalf("window=%d err=%v", got, err)
	}
	if _, err := selectNativeWindow(list, "editor.exe", "w-303"); err == nil {
		t.Fatal("window from another app accepted")
	}
	if _, err := selectNativeWindow(list, "other.exe", ""); err == nil {
		t.Fatal("minimized window accepted")
	}
}

func TestNativeServerAdvertisesToolsAndSupportsDenial(t *testing.T) {
	server, client := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ss, err := NewServer(CLI{Native: true}, DenyAll{}).Connect(ctx, server, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range listed.Tools {
		got[tool.Name] = true
	}
	for _, name := range []string{"desktop_windows", "desktop_screenshot", "desktop_snapshot", "desktop_click", "desktop_scroll", "desktop_drag", "desktop_type", "desktop_set_value", "desktop_press", "desktop_copy", "desktop_paste", "desktop_clipboard_read", "desktop_clipboard_write"} {
		if !got[name] {
			t.Errorf("missing %s", name)
		}
	}
	result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "desktop_clipboard_read", Arguments: map[string]any{}})
	if err != nil || !result.IsError || !strings.Contains(firstText(result), "did not allow") {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestNativeWindowCapture(t *testing.T) {
	type winMsg struct {
		hwnd           uintptr
		message        uint32
		wparam, lparam uintptr
		timestamp      uint32
		x, y           int32
		private        uint32
	}
	peek := user32.NewProc("PeekMessageW")
	dispatch := user32.NewProc("DispatchMessageW")
	ready := make(chan uintptr, 1)
	stop := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(exited)
		class, _ := windows.UTF16PtrFromString("STATIC")
		title, _ := windows.UTF16PtrFromString("Phoenix capture test")
		// An off-screen, non-activating window exercises PrintWindow without covering the user's apps.
		position := int32(-10000)
		hwnd, _, _ := procCreateWindow.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0x90000000, uintptr(uint32(position)), uintptr(uint32(position)), 240, 120, 0, 0, 0, 0)
		ready <- hwnd
		if hwnd == 0 {
			return
		}
		defer procDestroyWindow.Call(hwnd)
		for {
			select {
			case <-stop:
				return
			default:
			}
			var msg winMsg
			for {
				found, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1)
				if found == 0 {
					break
				}
				dispatch.Call(uintptr(unsafe.Pointer(&msg)))
			}
			time.Sleep(time.Millisecond)
		}
	}()
	hwnd := <-ready
	if hwnd == 0 {
		close(stop)
		<-exited
		t.Fatal("could not create the test window")
	}
	defer func() { close(stop); <-exited }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	windowsList, err := nativeWindows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "capture.png")
	env, handled, err := runNative(ctx, []string{"screenshot", "--app", filepath.Base(exe), "--window-id", fmt.Sprintf("w-%d", hwnd), "--", path})
	if err != nil || !handled || !env.OK {
		t.Fatalf("capture result=%+v handled=%v err=%v (windows=%d)", env, handled, err, len(windowsList))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 240 || img.Bounds().Dy() != 120 {
		t.Fatalf("bounds %v", img.Bounds())
	}
}
func TestNativeClipboardMemoryCopy(t *testing.T) {
	if err := procCopyMemory.Find(); err != nil {
		t.Fatal(err)
	}
	src := []uint16{65, 0x03a9, 0}
	dst := make([]uint16, len(src))
	procCopyMemory.Call(uintptr(unsafe.Pointer(&dst[0])), uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)*2))
	for i := range src {
		if src[i] != dst[i] {
			t.Fatalf("copied value at %d = %d, want %d", i, dst[i], src[i])
		}
	}
}
