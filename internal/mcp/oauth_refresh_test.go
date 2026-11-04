package mcp

import (
	"context"
	"fmt"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefreshIncludesMCPResource(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.Form.Get("resource")
		if r.Form.Get("grant_type") != "refresh_token" || got != "https://example.com/mcp" {
			http.Error(w, "invalid_target", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"new","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()
	cfg := &oauth2.Config{ClientID: "client", Endpoint: oauth2.Endpoint{TokenURL: srv.URL, AuthStyle: oauth2.AuthStyleInParams}}
	token, err := resourceTokenSource(context.Background(), cfg, &oauth2.Token{AccessToken: "expired", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)}, "https://example.com/mcp").Token()
	if err != nil || token.AccessToken != "new" || got != "https://example.com/mcp" {
		t.Fatalf("token=%v resource=%q err=%v", token, got, err)
	}
}
func TestRuntimeRefreshesLegacyOAuthRecordWithResource(t *testing.T) {
	mux := http.NewServeMux()
	var resource string
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("resource") != resource || r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, `{"error":"invalid_target"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"fresh-token","token_type":"Bearer","expires_in":3600}`)
	})
	upstream := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	endpoint := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return upstream }, nil)
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh-token" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="fixture"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		endpoint.ServeHTTP(w, r)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	resource = server.URL + "/mcp"
	record := OAuthRecord{ClientID: "client", Endpoint: oauth2.Endpoint{TokenURL: server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}, Token: &oauth2.Token{AccessToken: "expired", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)}}
	var saved OAuthRecord
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime, err := Open(ctx, []Resolved{{Server: Server{Name: "fixture", Transport: HTTP, URL: resource, Enabled: true}, OAuthRecord: &record, SaveOAuthRecord: func(r OAuthRecord) error { saved = r; return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if saved.Resource != resource || saved.Token == nil || saved.Token.AccessToken != "fresh-token" {
		t.Fatal("refreshed credentials or resource were not persisted")
	}
}
