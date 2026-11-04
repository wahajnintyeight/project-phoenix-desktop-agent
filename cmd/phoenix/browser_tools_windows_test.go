package main

import (
	"strconv"
	"strings"
	"testing"
)

func TestBrowserURLValidation(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://example.com/path?q=private", true},
		{"http://localhost:8080", true},
		{"file:///C:/Users/me/private.txt", false},
		{"javascript:alert(1)", false},
		{"https://user:password@example.com", false},
		{"https://example.com/a\nb", false},
	} {
		if got := validBrowserURL(tc.url); got != tc.want {
			t.Errorf("validBrowserURL(%q) = %t, want %t", tc.url, got, tc.want)
		}
	}
}

func TestBrowserKeysStayAllowlisted(t *testing.T) {
	for _, key := range []string{"Enter", "Tab", "Escape", "ArrowDown", "Ctrl+A", "Shift+Tab"} {
		if !validBrowserKey(key) {
			t.Errorf("key %q should be supported", key)
		}
	}
	for _, key := range []string{"Ctrl+V", "Alt+F4", "Ctrl+Shift+Escape", "Paste"} {
		if validBrowserKey(key) {
			t.Errorf("key %q should be refused", key)
		}
	}
}

func TestBrowserTextEntryBlocksSensitiveFields(t *testing.T) {
	for _, node := range []domNode{
		{LocalName: "input", Attributes: []string{"type", "password"}},
		{LocalName: "input", Attributes: []string{"type", "text", "autocomplete", "one-time-code"}},
		{LocalName: "input", Attributes: []string{"type", "text", "name", "cc_number"}},
		{LocalName: "input", Attributes: []string{"type", "file"}},
	} {
		if err := validateEditableNode(node); err == nil {
			t.Errorf("sensitive field was accepted: %#v", node.Attributes)
		}
	}
	if err := validateEditableNode(domNode{LocalName: "textarea"}); err != nil {
		t.Fatalf("ordinary textarea rejected: %v", err)
	}
	if err := validateEditableNode(domNode{LocalName: "div", Attributes: []string{"contenteditable", "true"}}); err != nil {
		t.Fatalf("ordinary contenteditable rejected: %v", err)
	}
}

func TestBrowserSnapshotCapsRefsAndText(t *testing.T) {
	nodes := make([]axNode, maxSnapshotNodes+50)
	for i := range nodes {
		nodes[i] = axNode{
			NodeID:        strconv.Itoa(i + 1),
			Role:          axValue{Value: "button"},
			Name:          axValue{Value: "Open"},
			BackendNodeID: i + 1,
		}
	}
	session := &browserSession{}
	out, err := formatBrowserSnapshot(browserTabState{ID: 4, Host: "example.com"}, 8, nodes, session)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.snapshot.refs) != maxSnapshotNodes {
		t.Fatalf("got %d refs, want %d", len(session.snapshot.refs), maxSnapshotNodes)
	}
	if len(out.Text) > maxBrowserText || !strings.Contains(out.Text, "capped at 250 nodes") {
		t.Fatalf("snapshot was not bounded: len=%d", len(out.Text))
	}
}
