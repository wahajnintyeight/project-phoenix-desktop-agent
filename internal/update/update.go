package update

import (
	_ "embed"
	"net/http"
	"time"
)

const ManifestURL = "https://github.com/wahajnintyeight/project-phoenix-desktop-agent/releases/latest/download/phoenix-update-manifest.json"

const (
	CheckTimeout    = 30 * time.Second
	DownloadTimeout = 30 * time.Minute
)

// HTTPClient allows large update bodies to stream without a short client-wide
// timeout. The caller bounds each operation with its own context deadline.
func HTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = CheckTimeout
	return &http.Client{Transport: transport}
}

//go:embed updater-public.pem
var PublicKey []byte
