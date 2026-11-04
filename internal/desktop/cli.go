// Package desktop exposes bounded native Windows computer-use tools through MCP.
package desktop

import (
	"context"
	"encoding/json"
	"errors"
)

// CLI dispatches native desktop commands.
type CLI struct {
	Native bool
	driver nativeDriver
}

// Envelope is the common result format for a desktop command.
type Envelope struct {
	OK      bool            `json:"ok"`
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		Suggestion string `json:"suggestion"`
	} `json:"error"`
}

// Err turns a failed envelope into an error whose text the model can act on.
func (e Envelope) Err() error {
	if e.OK {
		return nil
	}
	if e.Error == nil {
		return errors.New("the desktop tool failed")
	}
	msg := e.Error.Code + ": " + e.Error.Message
	if e.Error.Suggestion != "" {
		msg += " Suggestion: " + e.Error.Suggestion
	}
	return errors.New(msg)
}

// Run dispatches to the Windows implementation. The headed argument is retained by the tool interface.
func (c CLI) Run(ctx context.Context, _ bool, args ...string) (Envelope, error) {
	if c.driver != nil {
		if out, handled, err := c.driver.run(ctx, args); handled {
			return out, err
		}
	}
	if c.Native {
		if out, handled, err := runNative(ctx, args); handled {
			return out, err
		}
	}
	return Envelope{}, errors.New("this desktop action is unavailable")
}
