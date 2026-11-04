//go:build !windows

package desktop

import "context"

func runNative(context.Context, bool, []string) (Envelope, bool, error) {
	return Envelope{}, false, nil
}
