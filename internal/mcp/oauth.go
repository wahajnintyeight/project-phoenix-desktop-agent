package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

const (
	oauthClientIDURL = "https://raw.githubusercontent.com/wahajnintyeight/project-phoenix-desktop-agent/main/oauth/phoenix-client.json"
	oauthCallback    = "/oauth/callback"
)

// OAuthRecord keeps the refreshable token and the public OAuth client settings in one
// encrypted vault entry. Only its reference is stored in the MCP server config.
type OAuthRecord struct {
	ClientID     string          `json:"client_id"`
	ClientSecret string          `json:"client_secret,omitempty"`
	Endpoint     oauth2.Endpoint `json:"endpoint"`
	Scopes       []string        `json:"scopes,omitempty"`
	RedirectURL  string          `json:"redirect_url"`
	Token        *oauth2.Token   `json:"token"`
}

type persistedTokenSource struct {
	mu     sync.Mutex
	source oauth2.TokenSource
	record OAuthRecord
	last   string
	save   func(OAuthRecord) error
}

func (s *persistedTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, err := s.source.Token()
	if err != nil {
		if s.record.Token != nil && !s.record.Token.Valid() {
			if s.record.Token.RefreshToken == "" {
				return nil, &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
			}
			var retrieveErr *oauth2.RetrieveError
			if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
				return nil, err
			}
		}
		return nil, err
	}
	fingerprint := tokenFingerprint(token)
	if fingerprint != s.last {
		record := s.record
		record.Token = token
		if s.save != nil {
			if err := s.save(record); err != nil {
				return nil, fmt.Errorf("could not securely save OAuth credentials: %w", err)
			}
		}
		s.record, s.last = record, fingerprint
	}
	return token, nil
}

func tokenFingerprint(token *oauth2.Token) string {
	if token == nil {
		return ""
	}
	value := token.AccessToken + "\x00" + token.RefreshToken + "\x00" + token.Expiry.UTC().Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s Resolved) oauthHandler() (auth.OAuthHandler, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("could not start the sign-in callback: %w", err)
	}
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d%s", listener.Addr().(*net.TCPAddr).Port, oauthCallback)
	authorized := make(chan *auth.AuthorizationResult, 1)
	callbackErr := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(oauthCallback, func(w http.ResponseWriter, req *http.Request) {
		remote, _, splitErr := net.SplitHostPort(req.RemoteAddr)
		if req.Method != http.MethodGet || splitErr != nil || net.ParseIP(remote) == nil || !net.ParseIP(remote).IsLoopback() {
			http.Error(w, "Invalid sign-in callback.", http.StatusBadRequest)
			return
		}
		q := req.URL.Query()
		if authErr := q.Get("error"); authErr != "" {
			select {
			case callbackErr <- fmt.Errorf("authorization failed: %s", clip(authErr, 120)):
			default:
			}
			http.Error(w, "Sign-in was not completed. Return to Phoenix and try again.", http.StatusBadRequest)
			return
		}
		result := &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}
		if result.Code == "" || result.State == "" {
			http.Error(w, "The sign-in response was incomplete. Return to Phoenix and try again.", http.StatusBadRequest)
			return
		}
		select {
		case authorized <- result:
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("Sign-in complete. Return to Phoenix."))
		default:
			http.Error(w, "This sign-in response was already received.", http.StatusConflict)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case callbackErr <- err:
			default:
			}
		}
	}()
	cleanup := func() {
		_ = server.Close()
		_ = listener.Close()
	}

	fetchCode := func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		if err := openAuthorizationURL(args.URL); err != nil {
			return nil, err
		}
		select {
		case result := <-authorized:
			return result, nil
		case err := <-callbackErr:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	config := &auth.AuthorizationCodeHandlerConfig{
		ClientIDMetadataDocumentConfig: &auth.ClientIDMetadataDocumentConfig{URL: oauthClientIDURL},
		RedirectURL:                    redirectURL,
		AuthorizationCodeFetcher:       fetchCode,
		RequestRefreshToken:            true,
		Client:                         client,
		NewTokenSource: func(ctx context.Context, config *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
			record := recordFrom(config, token)
			return &persistedTokenSource{source: config.TokenSource(ctx, token), record: record, save: s.SaveOAuthRecord}, nil
		},
	}
	if s.OAuthRecord != nil && s.OAuthRecord.Token != nil {
		record := *s.OAuthRecord
		config.InitialTokenSource = &persistedTokenSource{
			source: oauthConfig(record).TokenSource(context.WithValue(context.Background(), oauth2.HTTPClient, client), record.Token),
			record: record,
			last:   tokenFingerprint(record.Token),
			save:   s.SaveOAuthRecord,
		}
	}
	handler, err := auth.NewAuthorizationCodeHandler(config)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return handler, cleanup, nil
}

func recordFrom(config *oauth2.Config, token *oauth2.Token) OAuthRecord {
	return OAuthRecord{
		ClientID: config.ClientID, ClientSecret: config.ClientSecret,
		Endpoint: config.Endpoint, Scopes: append([]string(nil), config.Scopes...),
		RedirectURL: config.RedirectURL, Token: token,
	}
}

func oauthConfig(record OAuthRecord) *oauth2.Config {
	return &oauth2.Config{
		ClientID: record.ClientID, ClientSecret: record.ClientSecret,
		Endpoint: record.Endpoint, Scopes: append([]string(nil), record.Scopes...),
		RedirectURL: record.RedirectURL,
	}
}

func openAuthorizationURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()))) {
		return errors.New("the authorization server returned an invalid sign-in URL")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", u.String())
	case "darwin":
		cmd = exec.Command("open", u.String())
	default:
		cmd = exec.Command("xdg-open", u.String())
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open the sign-in page: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func redactOAuthToken(message string, record *OAuthRecord) string {
	if record == nil || record.Token == nil {
		return message
	}
	for _, value := range []string{record.Token.AccessToken, record.Token.RefreshToken} {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[secret]")
		}
	}
	return message
}
