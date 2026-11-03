module phoenix

go 1.27.3

require (
	github.com/evanw/esbuild v0.28.2
	github.com/wailsapp/go-webview2 v1.0.23
	golang.org/x/sys v0.49.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/wailsapp/go-webview2 => ./third_party/go-webview2

require github.com/jchv/go-winloader v0.0.0-20210711035445-715c2860da7e // indirect
