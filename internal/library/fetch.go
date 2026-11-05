package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"
)

// allowPrivate lets tests fetch from a loopback server. Never set outside tests.
var allowPrivate = false

var cgnat = mustCIDR("100.64.0.0/10")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func publicIP(ip net.IP) bool {
	if allowPrivate {
		return true
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip))
}

func fetchClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
		// Control runs on the address actually being connected to, after DNS, so a name that
		// resolves to a private address (or is rebound to one) is refused too.
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !publicIP(ip) {
				return errors.New("that address points at this computer or a private network, which the agent may not fetch from")
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: 90 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			DisableKeepAlives:     true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !allowPrivate && req.URL.Scheme != "https" {
				return errors.New("redirected to an address that is not https")
			}
			return nil
		},
	}
}

// SaveURL downloads a file from an https address into the library and records where it came
// from. p may be empty (the name comes from the address, inside downloads/). The fetch carries
// no cookies or credentials: it is for links that work on their own, such as the signed address a
// tool returns for a generated document.
func (s *Store) SaveURL(ctx context.Context, rawURL, p, kind, note string) (string, int64, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "", 0, errors.New("that is not a valid address")
	}
	if u.Scheme != "https" && !(allowPrivate && u.Scheme == "http") {
		return "", 0, errors.New("only https addresses can be fetched")
	}
	if u.User != nil {
		return "", 0, errors.New("addresses with a username or password are not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "Phoenix-file-fetch/1")
	resp, err := fetchClient().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", 0, fmt.Errorf("could not download that address: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", 0, fmt.Errorf("the server answered %d, so nothing was saved", resp.StatusCode)
	}
	if p == "" {
		name := ""
		if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
			name = params["filename"]
		}
		if name == "" {
			name = path.Base(u.Path)
		}
		if name == "" || name == "/" || name == "." {
			name = "download"
		}
		p = "downloads/" + SafeName(name)
	}
	clean, err := Clean(p)
	if err != nil {
		return "", 0, err
	}
	if clean == "" {
		return "", 0, errors.New("give a file name")
	}
	if _, exists := s.lookup(clean); exists {
		return "", 0, fmt.Errorf("%s already exists; choose another name, or write over it only on purpose", clean)
	}
	if Dangerous(clean) {
		return "", 0, errors.New("programs and scripts cannot be downloaded into the library")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxDownload+1))
	if err != nil {
		return "", 0, fmt.Errorf("the download was interrupted: %v", err)
	}
	if len(data) > MaxDownload {
		return "", 0, fmt.Errorf("that file is larger than %d MB", MaxDownload>>20)
	}
	dir, name := split(clean)
	saved, err := s.Put(dir, name, data, "agent", u.String())
	if err != nil {
		return "", 0, err
	}
	if kind != "" || note != "" {
		_ = s.Annotate(saved, kind, note, "")
	}
	return saved, int64(len(data)), nil
}
