//go:build windows

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type testWinMsg struct {
	hwnd           uintptr
	message        uint32
	wparam, lparam uintptr
	timestamp      uint32
	x, y           int32
	private        uint32
}

func TestNativeAutomationSnapshotAndValue(t *testing.T) {
	type handles struct{ hwnd, edit uintptr }
	ready := make(chan handles, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		class, _ := windows.UTF16PtrFromString("STATIC")
		title, _ := windows.UTF16PtrFromString("Phoenix automation test")
		pos := int32(-10000)
		hwnd, _, _ := procCreateWindow.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0x90000000, uintptr(uint32(pos)), uintptr(uint32(pos)), 300, 150, 0, 0, 0, 0)
		var editHandle uintptr
		if hwnd != 0 {
			edit, _ := windows.UTF16PtrFromString("EDIT")
			initial, _ := windows.UTF16PtrFromString("initial")
			editHandle, _, _ = procCreateWindow.Call(0, uintptr(unsafe.Pointer(edit)), uintptr(unsafe.Pointer(initial)), 0x50000000, 10, 10, 140, 30, hwnd, 0, 0, 0)
			button, _ := windows.UTF16PtrFromString("BUTTON")
			label, _ := windows.UTF16PtrFromString("Do action")
			procCreateWindow.Call(0, uintptr(unsafe.Pointer(button)), uintptr(unsafe.Pointer(label)), 0x50000000, 10, 55, 110, 30, hwnd, 0, 0, 0)
		}
		ready <- handles{hwnd: hwnd, edit: editHandle}
		if hwnd == 0 {
			return
		}
		defer procDestroyWindow.Call(hwnd)
		peek := user32.NewProc("PeekMessageW")
		dispatch := user32.NewProc("DispatchMessageW")
		for {
			select {
			case <-stop:
				return
			default:
			}
			var msg testWinMsg
			for {
				ok, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1)
				if ok == 0 {
					break
				}
				dispatch.Call(uintptr(unsafe.Pointer(&msg)))
			}
			time.Sleep(time.Millisecond)
		}
	}()
	testWindow := <-ready
	hwnd := testWindow.hwnd
	if hwnd == 0 || testWindow.edit == 0 {
		close(stop)
		<-done
		t.Fatal("cannot create test window")
	}
	defer func() { close(stop); <-done }()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	driver := newNativeDriver()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := nativeClickAt(ctx, filepath.Base(exe), fmt.Sprintf("w-%d", hwnd), "400", "20"); err == nil {
		t.Fatal("out-of-window coordinate was accepted")
	}
	args := []string{"snapshot", "--app", filepath.Base(exe), "--window-id", fmt.Sprintf("w-%d", hwnd)}
	env, handled, err := driver.run(ctx, args)
	if err != nil || !handled {
		t.Fatalf("snapshot: %v", err)
	}
	var out struct {
		Tree node `json:"tree"`
	}
	if err = json.Unmarshal(env.Data, &out); err != nil {
		t.Fatal(err)
	}
	var editRef, buttonRef string
	for _, item := range out.Tree.Children {
		if item.Name == "Do action" {
			buttonRef = item.Ref
		}
		if strings.Contains(strings.ToLower(item.Role), "edit") {
			editRef = item.Ref
			if item.Value != "initial" {
				t.Errorf("edit value = %q", item.Value)
			}
		}
	}
	if editRef == "" || buttonRef == "" {
		t.Fatalf("missing controls: %+v", out.Tree.Children)
	}
	_, _, err = driver.run(ctx, []string{"click", "--", buttonRef})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	_, _, err = driver.run(ctx, []string{"set-value", "--", editRef, "changed"})
	if err != nil {
		t.Fatalf("set value: %v", err)
	}
	var value [64]uint16
	procGetWindowText.Call(testWindow.edit, uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)))
	if got := windows.UTF16ToString(value[:]); got != "changed" {
		t.Fatalf("field value = %q", got)
	}
	_, _, err = driver.run(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = driver.run(ctx, []string{"set-value", "--", editRef, "stale"})
	if err == nil || !strings.Contains(err.Error(), "STALE_REF") {
		t.Fatalf("old ref err=%v", err)
	}
}
func TestNativeShortcutValidation(t *testing.T) {
	for _, key := range []string{"ctrl+c", "shift+tab", "ctrl+shift+a", "enter"} {
		if _, err := parseKeys(key); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
	for _, key := range []string{"ctrl+ctrl+a", "ctrl+unknown", "alt+"} {
		if _, err := parseKeys(key); err == nil {
			t.Errorf("accepted %s", key)
		}
	}
}
func TestUnicodeKeyRecords(t *testing.T) {
	down := unicodeKey(0x03a9, false)
	up := unicodeKey(0x03a9, true)
	a := (*keyInput)(unsafe.Pointer(&down.payload[0]))
	b := (*keyInput)(unsafe.Pointer(&up.payload[0]))
	if down.kind != inputKeyboard || a.scan != 0x03a9 || a.flags != keyUnicode || b.flags != keyUnicode|keyReleased {
		t.Fatal("invalid unicode input records")
	}
	if unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(inputRecord{}) != 40 {
		t.Fatalf("input size %d", unsafe.Sizeof(inputRecord{}))
	}
}
