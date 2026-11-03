module phoenix

go 1.27.3

require (
	github.com/evanw/esbuild v0.28.2
	github.com/wailsapp/go-webview2 v1.0.23
	github.com/wailsapp/wails/v3 v3.1.0
	golang.org/x/sys v0.49.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/wailsapp/go-webview2 => ./third_party/go-webview2

require (
	github.com/coder/websocket v1.8.14 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jchv/go-winloader v0.0.0-20210711035445-715c2860da7e // indirect
	golang.org/x/term v0.46.0 // indirect
)
