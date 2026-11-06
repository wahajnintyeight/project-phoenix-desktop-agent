package library

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) == 0 {
		return ""
	}
	return res.Content[0].(*sdk.TextContent).Text
}

func TestApplicationsAreLoggedOnceAndRemembered(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, b := sdk.NewInMemoryTransports()
	ss, err := NewServer(st, "agent").Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "1"}, nil).Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	callTool(t, cs, "applications_log", map[string]any{"company": "Lorven", "role": "Full-stack", "url": "https://x/1", "status": "started"})
	callTool(t, cs, "applications_log", map[string]any{"company": "Lorven", "role": "Full-stack", "url": "https://x/1", "status": "applied"})
	list := callTool(t, cs, "applications_list", map[string]any{"query": "lorven"})
	if strings.Count(list, "Lorven") != 1 || !strings.Contains(list, "applied:") {
		t.Fatalf("same address should update one entry: %q", list)
	}
	if got := callTool(t, cs, "applications_log", map[string]any{"company": "A", "role": "B", "status": "maybe"}); !strings.Contains(got, "status must be") {
		t.Fatalf("bad status accepted: %q", got)
	}

	callTool(t, cs, "memory_remember", map[string]any{"topic": "Work authorisation", "key": "Authorised to work in the UK?", "value": "Yes, no sponsorship", "kind": "answer"})
	callTool(t, cs, "memory_remember", map[string]any{"topic": "Work authorisation", "key": "Authorised to work in the UK?", "value": "Yes, no sponsorship", "kind": "answer"})
	ms, _ := st.Memories()
	if len(ms) != 1 || ms[0].Uses != 2 {
		t.Fatalf("same key should update one entry and count the reuse: %+v", ms)
	}
	if got := callTool(t, cs, "memory_recall", map[string]any{"query": "uk"}); !strings.Contains(got, "sponsorship") {
		t.Fatalf("recall = %q", got)
	}
	callTool(t, cs, "memory_forget", map[string]any{"query": "uk"})
	if ms, _ := st.Memories(); len(ms) != 0 {
		t.Fatalf("forget left %+v", ms)
	}
}
