//go:build windows

package desktop

import (
	"errors"
	"runtime"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	ntdll                = windows.NewLazySystemDLL("ntdll.dll")
	procCopyMemory       = ntdll.NewProc("RtlMoveMemory")
	procCreateWindow     = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalSize       = kernel32.NewProc("GlobalSize")
)

const cfUnicodeText = 13

func nativeClipboardRead() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	opened, _, _ := procOpenClipboard.Call(0)
	if opened == 0 {
		return "", errors.New("clipboard is busy; try again")
	}
	defer procCloseClipboard.Call()
	handle, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if handle == 0 {
		return "", nil
	}
	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 || size > 128<<10 {
		return "", errors.New("clipboard text is unavailable or too large")
	}
	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return "", errors.New("could not read the clipboard")
	}
	defer procGlobalUnlock.Call(handle)
	data := make([]uint16, int(size/2))
	if len(data) > 0 {
		procCopyMemory.Call(uintptr(unsafe.Pointer(&data[0])), ptr, uintptr(len(data)*2))
	}
	end := 0
	for end < len(data) && data[end] != 0 {
		end++
	}
	if end == len(data) {
		return "", errors.New("clipboard text is not terminated")
	}
	return string(utf16.Decode(data[:end])), nil
}

func nativeClipboardWrite(text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if text == "" || len(text) > maxText {
		return errors.New("clipboard text is empty or too large")
	}
	chars, err := windows.UTF16FromString(text)
	if err != nil {
		return errors.New("clipboard text contains a null character")
	}
	handle, _, _ := procGlobalAlloc.Call(2, uintptr(len(chars)*2))
	if handle == 0 {
		return errors.New("could not allocate clipboard data")
	}
	owned := true
	defer func() {
		if owned {
			procGlobalFree.Call(handle)
		}
	}()
	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return errors.New("could not prepare clipboard data")
	}
	procCopyMemory.Call(ptr, uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)*2))
	procGlobalUnlock.Call(handle)
	className, _ := windows.UTF16PtrFromString("STATIC")
	owner, _, _ := procCreateWindow.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0)
	if owner == 0 {
		return errors.New("could not create a clipboard owner")
	}
	defer procDestroyWindow.Call(owner)
	opened, _, _ := procOpenClipboard.Call(owner)
	if opened == 0 {
		return errors.New("clipboard is busy; try again")
	}
	defer procCloseClipboard.Call()
	ok, _, _ := procEmptyClipboard.Call()
	if ok == 0 {
		return errors.New("could not clear the clipboard")
	}
	result, _, _ := procSetClipboardData.Call(cfUnicodeText, handle)
	if result == 0 {
		return errors.New("could not write the clipboard")
	}
	owned = false
	return nil
}
