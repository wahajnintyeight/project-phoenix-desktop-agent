package mcp

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// OAuth's standard token source does not carry RFC 8707's resource parameter
// into refresh requests. Add it only to refreshes to the configured token endpoint.
type resourceRefreshTransport struct {
	endpoint, resource string
	next               http.RoundTripper
}

func (t resourceRefreshTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.URL.String() != t.endpoint || t.resource == "" || !strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		return t.next.RoundTrip(req)
	}
	defer req.Body.Close()
	body, err := io.ReadAll(io.LimitReader(req.Body, 64<<10+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 64<<10 {
		return nil, io.ErrShortBuffer
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	if values.Get("grant_type") == "refresh_token" && values.Get("resource") == "" {
		values.Set("resource", t.resource)
	}
	clone := req.Clone(req.Context())
	data := values.Encode()
	clone.Body = io.NopCloser(strings.NewReader(data))
	clone.ContentLength = int64(len(data))
	return t.next.RoundTrip(clone)
}

func resourceTokenSource(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token, resource string) oauth2.TokenSource {
	client := &http.Client{Timeout: 30 * time.Second, Transport: resourceRefreshTransport{endpoint: cfg.Endpoint.TokenURL, resource: resource, next: http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return cfg.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, client), token)
}
