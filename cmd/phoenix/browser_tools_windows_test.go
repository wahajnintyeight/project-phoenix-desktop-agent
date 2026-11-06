package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"phoenix/internal/browser"
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
	out, err := formatBrowserSnapshot(browserTabState{ID: 4, Host: "example.com"}, "", nodes, session, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.snapshot.refs) != maxSnapshotNodes {
		t.Fatalf("got %d refs, want %d", len(session.snapshot.refs), maxSnapshotNodes)
	}
	if len(out.Text) > maxBrowserText || !strings.Contains(out.Text, "more controls not listed") {
		t.Fatalf("snapshot was not bounded: len=%d", len(out.Text))
	}
}
func TestBrowserDragArgumentsRequireBothSnapshotRefs(t *testing.T) {
	good := browser.Call{Tool: "browser_drag", Arguments: []byte(`{"tab_id":1,"from_ref":"@s12345:e1","to_ref":"@s12345:e2"}`)}
	if _, err := decodeBrowserCall(good); err != nil {
		t.Fatal(err)
	}
	good.Arguments = []byte(`{"tab_id":1,"from_ref":"@s12345:e1","to_ref":"missing"}`)
	if _, err := decodeBrowserCall(good); err == nil {
		t.Fatal("invalid destination ref accepted")
	}
}

// Every tool the MCP server publishes must be accepted by the host bridge and decoded by
// decodeBrowserCall; a tool added to one list and not the other fails silently at run time.
func TestEveryPublishedBrowserToolIsHandledByTheHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := browser.NewMCPServer(nil).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) == 0 {
		t.Fatal("no tools published")
	}
	for _, tool := range listed.Tools {
		if !knownBrowserTool(tool.Name) {
			t.Errorf("%s is published to the model but rejected by the host bridge", tool.Name)
		}
		// An unknown tool name is the only decode error this probe may produce.
		if _, err := decodeBrowserCall(browser.Call{Tool: tool.Name, Arguments: []byte(`{}`)}); err != nil && err.Error() == "unknown browser operation" {
			t.Errorf("%s has no argument decoder", tool.Name)
		}
	}
}

// A control buried deep in a busy page (a job's Apply link behind hundreds of other controls)
// must still be findable.
func TestBrowserSnapshotQueryFindsControlPastTheCap(t *testing.T) {
	nodes := make([]axNode, 0, maxSnapshotNodes+100)
	for i := 0; i < maxSnapshotNodes+50; i++ {
		nodes = append(nodes, axNode{NodeID: strconv.Itoa(i + 1), Role: axValue{Value: "link"}, Name: axValue{Value: "Job " + strconv.Itoa(i)}, BackendNodeID: i + 1})
	}
	nodes = append(nodes, axNode{NodeID: "9999", Role: axValue{Value: "link"}, Name: axValue{Value: "Apply"}, BackendNodeID: 9999})
	session := &browserSession{}
	out, err := formatBrowserSnapshot(browserTabState{ID: 4, Host: "example.com"}, "apply", nodes, session, nil)
	if err != nil || !strings.Contains(out.Text, `link "Apply" ref=`) || len(session.snapshot.refs) != 1 {
		t.Fatalf("query did not find Apply: %v\n%s", err, out.Text)
	}
}
