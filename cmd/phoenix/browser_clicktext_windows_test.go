package main

import "testing"

func TestBestClickTarget(t *testing.T) {
	refs := map[string]browserRef{
		"@sa:e1": {Role: "link", Name: "Apply filters"},
		"@sa:e2": {Role: "textbox", Name: "Apply"},
		"@sa:e3": {Role: "button", Name: "Apply"},
		"@sa:e9": {Role: "button", Name: "Apply"},
		"@sa:e4": {Role: "button", Name: "Save job"},
	}
	if got, ok := bestClickTarget(refs, " apply "); !ok || got != "@sa:e3" {
		t.Fatalf("exact button should win, earliest first: got %q %v", got, ok)
	}
	if got, ok := bestClickTarget(refs, "filters"); !ok || got != "@sa:e1" {
		t.Fatalf("contains match: got %q %v", got, ok)
	}
	if _, ok := bestClickTarget(refs, "Submit"); ok {
		t.Fatal("no control says Submit")
	}
	if _, ok := bestClickTarget(refs, "  "); ok {
		t.Fatal("an empty label must match nothing")
	}
	if refIndexOf("@sa:e12") != 12 {
		t.Fatal("ref index")
	}
}
