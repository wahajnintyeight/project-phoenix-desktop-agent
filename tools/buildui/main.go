// buildui bundles the TypeScript in ./ui with esbuild, takes the CSS that Tailwind compiled
// (ui/.cache/app.css; scripts/build.ps1 runs Tailwind first) and writes a single
// self-contained index.html (inline script and style, CSP hashes computed) to
// internal/ui/dist/index.html, which the Go host embeds.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "buildui:", err)
		os.Exit(1)
	}
}

func bundle(opts api.BuildOptions) (string, error) {
	opts.Write = false
	opts.Bundle = true
	opts.MinifyWhitespace = true
	opts.MinifyIdentifiers = true
	opts.MinifySyntax = true
	opts.LogLevel = api.LogLevelWarning
	res := api.Build(opts)
	if len(res.Errors) > 0 {
		var sb strings.Builder
		for _, m := range res.Errors {
			sb.WriteString(m.Text)
			sb.WriteString("\n")
		}
		return "", fmt.Errorf("%s", sb.String())
	}
	if len(res.OutputFiles) != 1 {
		return "", fmt.Errorf("expected 1 output file, got %d", len(res.OutputFiles))
	}
	return string(res.OutputFiles[0].Contents), nil
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func run() error {
	js, err := bundle(api.BuildOptions{
		EntryPoints: []string{"ui/src/app.ts"},
		Format:      api.FormatIIFE,
		Target:      api.ES2022,
		Outfile:     "app.js",
	})
	if err != nil {
		return err
	}
	// Tailwind already produced minified plain CSS; it is inlined as it is.
	cssBytes, err := os.ReadFile(filepath.Join("ui", ".cache", "app.css"))
	if err != nil {
		return fmt.Errorf("compiled CSS not found (%v); run scripts/build.ps1, or `npm run build:css` first", err)
	}
	css := string(cssBytes)
	tpl, err := os.ReadFile("ui/index.html")
	if err != nil {
		return err
	}
	// "</script" or "</style" inside inline content would end the element early.
	if strings.Contains(strings.ToLower(js), "</script") || strings.Contains(strings.ToLower(css), "</style") {
		return fmt.Errorf("bundle contains a closing tag sequence; cannot inline safely")
	}
	html := string(tpl)
	html = strings.ReplaceAll(html, "{{SCRIPT_HASH}}", hash(js))
	html = strings.ReplaceAll(html, "{{STYLE_HASH}}", hash(css))
	html = strings.ReplaceAll(html, "{{CSS}}", css)
	html = strings.ReplaceAll(html, "{{JS}}", js)

	dst := filepath.Join("internal", "ui", "dist", "index.html")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(html), 0o644); err != nil {
		return err
	}
	fmt.Printf("buildui: wrote %s (%d bytes; js %d, css %d)\n", dst, len(html), len(js), len(css))
	return nil
}
