//go:build windows

package main

import (
	"os"
	"runtime"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Set PHOENIX_LIVE_TOAST_TEST=1 to send a real Windows notification from the
// current desktop session. This is opt-in because it shows a visible toast.
func TestLiveWindowsToast(t *testing.T) {
	if os.Getenv("PHOENIX_LIVE_TOAST_TEST") != "1" {
		t.Skip("set PHOENIX_LIVE_TOAST_TEST=1 to send a real Windows toast")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr := w32.CoInitializeEx(w32.COINIT_APARTMENTTHREADED); hr != w32.S_OK && hr != w32.S_FALSE {
		t.Fatalf("CoInitializeEx failed: %#x", uint32(hr))
	}
	defer w32.CoUninitialize()

	a := application.New(application.Options{Name: "Phoenix"})
	lifecycle := &notificationLifecycle{}
	if err := lifecycle.ServiceStartup(a.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	if !notificationsAvailable {
		t.Fatalf("notification service failed to start: %s", notificationStartupError)
	}
	t.Cleanup(func() { _ = lifecycle.ServiceShutdown() })

	oldData := notificationData
	t.Cleanup(func() { notificationData = oldData })
	notificationData.Settings.Enabled = true
	result := sendTestNotification()
	if !result.OK {
		t.Fatal(result.Message)
	}
	t.Log(result.Message)
}
