package voice

import (
	"context"
	"errors"
)

// Provider keeps speech services independent from the chat provider stack.
type Provider interface {
	Transcribe(context.Context, string, []byte, string) (string, error)
	Speak(context.Context, string, string) ([]byte, error)
}

func New(id string) (Provider, error) {
	switch id {
	case "cartesia":
		return Cartesia{}, nil
	default:
		return nil, errors.New("unsupported speech provider")
	}
}
