package update

import _ "embed"

const ManifestURL = "https://github.com/wahajnintyeight/project-phoenix-desktop-agent/releases/latest/download/phoenix-update-manifest.json"

//go:embed updater-public.pem
var PublicKey []byte
