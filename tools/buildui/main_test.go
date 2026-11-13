package main

import (
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

func TestBundleRegistersMentionPicker(t *testing.T) {
	js, err := bundle(api.BuildOptions{
		EntryPoints: []string{"../../ui/src/app.ts"}, Format: api.FormatIIFE, Target: api.ES2022,
		Outfile: "app.js", MinifyWhitespace: true, MinifyIdentifiers: true, MinifySyntax: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, `customElements.define("ph-mentions",`) {
		t.Fatal("the bundled chat must register the mention picker before constructing it")
	}
}
