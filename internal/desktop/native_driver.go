package desktop

import "context"

type nativeDriver interface {
	run(context.Context, []string) (Envelope, bool, error)
}
