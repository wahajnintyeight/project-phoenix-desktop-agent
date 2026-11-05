//go:build !windows

package desktop

import "context"

func runNative(context.Context, []string) (Envelope, bool, error) {
	return Envelope{}, false, nil
}
func newNativeDriver() nativeDriver { return nil }
func warmDriver(nativeDriver)       {}

func clipboardSequence() uint32                      { return 0 }
func waitForClipboard(context.Context, uint32) error { return nil }
