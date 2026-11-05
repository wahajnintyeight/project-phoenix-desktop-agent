// Package browser contains Phoenix's browser-agent MCP surface and its authenticated
// localhost bridge to the WebView2 host. It never exposes raw CDP commands to the model.
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ServerName = "phoenix-browser"
	toolPath   = "/browser/tool"
	maxRequest = 32 << 10
	maxReply   = 5 << 20
	maxText    = 24 << 10
	maxImage   = 4 << 20
)

// Call is a validated, named browser operation. Arguments are JSON because each MCP tool
// has its own schema; the host validates them again before touching WebView2.
type Call struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

// Output is the bounded result returned by the host. Screenshots use one image part.
type Output struct {
	Text     string `json:"text"`
	Image    []byte `json:"image,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	IsError  bool   `json:"is_error,omitempty"`
}

// Invoker sends one MCP operation to the running Phoenix window.
type Invoker interface {
	Invoke(context.Context, Call) (Output, error)
}

// Client is the stdio MCP child's authenticated connection to Phoenix.
type Client struct {
	URL   string
	Token string
	HTTP  *http.Client
}

func (c Client) Invoke(ctx context.Context, call Call) (Output, error) {
	if !localBridgeURL(c.URL) || len(c.Token) < 32 {
		return Output{}, errors.New("Phoenix browser bridge is unavailable")
	}
	body, err := json.Marshal(call)
	if err != nil || len(body) > maxRequest {
		return Output{}, errors.New("browser request is invalid or too large")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return Output{}, errors.New("could not create browser request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 6 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Output{}, fmt.Errorf("Phoenix browser connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Output{}, errors.New("Phoenix could not complete the browser operation")
	}
	var out Output
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReply)).Decode(&out); err != nil {
		return Output{}, errors.New("Phoenix returned an invalid browser result")
	}
	if err := out.Check(); err != nil {
		return Output{}, err
	}
	return out, nil
}

// Check enforces the size and type limits on a result before it reaches the model.
func (o Output) Check() error {
	if len(o.Text) > maxText || len(o.Image) > maxImage || (len(o.Image) > 0 && o.MIMEType != "image/jpeg" && o.MIMEType != "image/png") {
		return errors.New("Phoenix browser result exceeded its limit")
	}
	return nil
}

func localBridgeURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != toolPath || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return strings.EqualFold(u.Hostname(), "127.0.0.1")
}

func RemoteHostIsLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.Host)
	return err == nil && host == "127.0.0.1"
}
