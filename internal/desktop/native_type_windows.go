//go:build windows

package desktop

import (
	"context"
	"errors"
	"unicode/utf16"
	"unsafe"

	uia "github.com/auuunya/go-element"
)

var procSendInput = user32.NewProc("SendInput")

const (
	inputKeyboard = 1
	keyUnicode    = 4
	keyReleased   = 2
)

type keyInput struct {
	vk, scan    uint16
	flags, time uint32
	extra       uintptr
}
type inputRecord struct {
	kind    uint32
	padding uint32
	payload [4]uintptr
}

func unicodeKey(ch uint16, up bool) inputRecord {
	record := inputRecord{kind: inputKeyboard}
	key := (*keyInput)(unsafe.Pointer(&record.payload[0]))
	key.scan = ch
	key.flags = keyUnicode
	if up {
		key.flags |= keyReleased
	}
	return record
}

func virtualKey(key byte, up bool) inputRecord {
	record := inputRecord{kind: inputKeyboard}
	input := (*keyInput)(unsafe.Pointer(&record.payload[0]))
	input.vk = uint16(key)
	if up {
		input.flags = keyReleased
	}
	return record
}

func sendVirtualKeys(keys []byte) error {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return errors.New("native keyboard input requires a 64-bit Windows build")
	}
	var batch [8]inputRecord
	n := 0
	for _, key := range keys {
		batch[n] = virtualKey(key, false)
		n++
	}
	for i := len(keys) - 1; i >= 0; i-- {
		batch[n] = virtualKey(keys[i], true)
		n++
	}
	sent, _, _ := procSendInput.Call(uintptr(n), uintptr(unsafe.Pointer(&batch[0])), unsafe.Sizeof(inputRecord{}))
	if int(sent) != n {
		var releases [4]inputRecord
		for i := range keys {
			releases[i] = virtualKey(keys[len(keys)-1-i], true)
		}
		procSendInput.Call(uintptr(len(keys)), uintptr(unsafe.Pointer(&releases[0])), unsafe.Sizeof(inputRecord{}))
		return errors.New("the key press was interrupted; inspect the app before retrying")
	}
	return nil
}

func (s *automationState) typeText(ctx context.Context, el *uia.IUIAutomationElement, value string) error {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return errors.New("native typing requires a 64-bit Windows build")
	}
	if err := foreground(s.hwnd); err != nil {
		return err
	}
	if err := el.SetFocus(); err != nil {
		return errors.New("could not focus the target field")
	}
	chars := utf16.Encode([]rune(value))
	for offset := 0; offset < len(chars); {
		if err := ctx.Err(); err != nil {
			return err
		}
		active, _, _ := procGetForegroundWindow.Call()
		if active != s.hwnd {
			return errors.New("focus changed during typing; inspect the field before retrying")
		}
		var batch [64]inputRecord
		start := offset
		n := 0
		for offset < len(chars) && n+2 <= len(batch) {
			batch[n] = unicodeKey(chars[offset], false)
			batch[n+1] = unicodeKey(chars[offset], true)
			n += 2
			offset++
		}
		sent, _, _ := procSendInput.Call(uintptr(n), uintptr(unsafe.Pointer(&batch[0])), unsafe.Sizeof(inputRecord{}))
		if int(sent) != n {
			if sent%2 == 1 && start+int(sent)/2 < len(chars) {
				release := unicodeKey(chars[start+int(sent)/2], true)
				procSendInput.Call(1, uintptr(unsafe.Pointer(&release)), unsafe.Sizeof(inputRecord{}))
			}
			return errors.New("typing was interrupted; inspect the field before retrying")
		}
	}
	return nil
}
