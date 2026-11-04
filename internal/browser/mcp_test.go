package browser

import (
	"context"
	"sort"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type unavailableInvoker struct{}

func (unavailableInvoker) Invoke(context.Context, Call) (Output, error) {
	return Output{}, context.DeadlineExceeded
}

func TestMCPServerPublishesBrowserTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server, client := sdk.NewInMemoryTransports()
	serverSession, err := NewMCPServer(unavailableInvoker{}).Connect(ctx, server, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	want := []string{
		"browser_back", "browser_click", "browser_close_tab", "browser_copy", "browser_drag", "browser_paste", "browser_fill", "browser_forward",
		"browser_navigate", "browser_open_tab", "browser_press", "browser_reload", "browser_scroll",
		"browser_screenshot", "browser_select", "browser_snapshot", "browser_switch_tab", "browser_tabs", "browser_type", "browser_wait",
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("published %d tools, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool %d = %q, want %q", i, got[i], want[i])
		}
	}
}
