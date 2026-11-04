package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fixtureDriver struct{ log string }

func (f fixtureDriver) run(_ context.Context, args []string) (Envelope, bool, error) {
	file, err := os.OpenFile(f.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return Envelope{}, true, err
	}
	fmt.Fprintln(file, strings.Join(args, " "))
	_ = file.Close()
	raw := `{"ok":true,"data":{}}`
	switch args[0] {
	case "list-windows":
		raw = `{"ok":true,"data":[{"app_name":"notepad.exe","title":"Untitled","id":"w-11","is_focused":true},{"app_name":"slack.exe","title":"General","id":"w-12","minimized":true}]}`
	case "screenshot":
		_ = os.WriteFile(args[len(args)-1], []byte("\x89PNG-fake"), 0600)
	case "snapshot":
		raw = `{"ok":true,"data":{"snapshot_id":"s1","tree":{"role":"window","children":[{"ref_id":"@s1:e1","role":"button","name":"Send","states":["focused"]},{"role":"group","children":[{"ref_id":"@s1:e2","role":"edit","name":"Message","value":"hi"}]}]}}}`
	case "clipboard-get":
		raw = `{"ok":true,"data":{"text":"copied words","type":"text"}}`
	case "click":
		raw = `{"ok":false,"error":{"code":"STALE_REF","message":"gone","suggestion":"snapshot again"}}`
	}
	var env Envelope
	err = json.Unmarshal([]byte(raw), &env)
	return env, true, err
}

type recorder struct {
	mu   sync.Mutex
	reqs []Request
	yes  bool
}

func (r *recorder) Approve(_ context.Context, q Request) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, q)
	return r.yes, nil
}

func session(t *testing.T, a Approver) (*sdk.ClientSession, string) {
	t.Helper()
	log := t.TempDir() + "/args.log"
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	srv := NewServer(CLI{driver: fixtureDriver{log: log}}, a)
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, log
}

func call(t *testing.T, cs *sdk.ClientSession, name string, args any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func firstText(r *sdk.CallToolResult) string {
	for _, c := range r.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func TestLookToolsAndApprovalWording(t *testing.T) {
	rec := &recorder{yes: true}
	cs, log := session(t, rec)

	w := call(t, cs, "desktop_windows", map[string]any{})
	if w.IsError || !strings.Contains(firstText(w), "w-11  app=notepad.exe") || !strings.Contains(firstText(w), "[minimized]") {
		t.Fatalf("%+v", w)
	}
	shot := call(t, cs, "desktop_screenshot", map[string]any{"app": "notepad.exe"})
	var img *sdk.ImageContent
	for _, c := range shot.Content {
		if ic, ok := c.(*sdk.ImageContent); ok {
			img = ic
		}
	}
	if shot.IsError || img == nil || string(img.Data) != "\x89PNG-fake" || img.MIMEType != "image/png" {
		t.Fatalf("%+v", shot)
	}
	snap := call(t, cs, "desktop_snapshot", map[string]any{"app": "notepad.exe"})
	txt := firstText(snap)
	if !strings.Contains(txt, `@s1:e1 button "Send" [focused]`) || !strings.Contains(txt, `@s1:e2 edit "Message" value="hi"`) {
		t.Fatalf("%s", txt)
	}
	// Refs from the snapshot now give the approval prompt words, not ids.
	call(t, cs, "desktop_type", map[string]any{"ref": "@s1:e2", "text": "hello"})
	last := rec.reqs[len(rec.reqs)-1]
	if last.Class != Act || last.Summary != "Type “hello” into edit “Message” (uses your keyboard)" {
		t.Fatalf("%+v", last)
	}
	if rec.reqs[1].Class != Look || rec.reqs[1].App != "notepad.exe" {
		t.Fatalf("%+v", rec.reqs[1])
	}
	args, _ := os.ReadFile(log)
	for _, want := range []string{"screenshot --app notepad.exe -- ", "snapshot --app notepad.exe -i --compact", "type -- @s1:e2 hello"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("missing %q in:\n%s", want, args)
		}
	}
}

func TestDeclinedActionNeverRuns(t *testing.T) {
	rec := &recorder{yes: false}
	cs, log := session(t, rec)
	r := call(t, cs, "desktop_clipboard_read", map[string]any{})
	if !r.IsError || !strings.Contains(firstText(r), "did not allow") {
		t.Fatalf("%+v", r)
	}
	r = call(t, cs, "desktop_press", map[string]any{"keys": "ctrl+v", "app": "notepad.exe"})
	if !r.IsError {
		t.Fatal("press should be refused")
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Fatalf("the desktop driver ran after a refusal:\n%s", b)
	}
	if len(rec.reqs) != 2 {
		t.Fatalf("%+v", rec.reqs)
	}
}

func TestArgumentsAreChecked(t *testing.T) {
	rec := &recorder{yes: true}
	cs, log := session(t, rec)
	bad := []struct {
		tool string
		args map[string]any
	}{
		{"desktop_click", map[string]any{"ref": "@e5"}},
		{"desktop_click", map[string]any{"ref": "--force"}},
		{"desktop_click", map[string]any{"ref": "@s1:e1 --headed"}},
		{"desktop_screenshot", map[string]any{"app": "--screenshot"}},
		{"desktop_screenshot", map[string]any{"app": "a; rm -rf"}},
		{"desktop_screenshot", map[string]any{"app": "ok.exe", "window_id": "w-1 --x"}},
		{"desktop_press", map[string]any{"keys": "--force", "app": "x.exe"}},
		{"desktop_press", map[string]any{"keys": "ctrl+v", "app": ""}},
		{"desktop_scroll", map[string]any{"ref": "@s1:e1", "direction": "sideways"}},
		{"desktop_scroll", map[string]any{"ref": "@s1:e1", "amount": 9999}},
		{"desktop_drag", map[string]any{"from_ref": "@s1:e1"}},
		{"desktop_drag", map[string]any{"from_xy": "1,2", "to_xy": "3;4"}},
		{"desktop_clipboard_write", map[string]any{"text": ""}},
		{"desktop_type", map[string]any{"ref": "@s1:e1", "text": strings.Repeat("a", maxText+1)}},
	}
	for _, c := range bad {
		r := call(t, cs, c.tool, c.args)
		if !r.IsError {
			t.Errorf("%s %v was accepted", c.tool, c.args)
		}
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("invalid calls must not reach the user: %+v", rec.reqs)
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Fatalf("the desktop driver ran for invalid input:\n%s", b)
	}
	// Text that looks like a flag is passed after "--", so it is data to the driver.
	call(t, cs, "desktop_set_value", map[string]any{"ref": "@s1:e1", "text": "--headed"})
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "set-value -- @s1:e1 --headed") {
		t.Fatalf("%s", b)
	}
}

func TestCopyReadsClipboardAndCLIErrorsReachTheModel(t *testing.T) {
	cs, _ := session(t, &recorder{yes: true})
	r := call(t, cs, "desktop_copy", map[string]any{"app": "notepad.exe"})
	if r.IsError || !strings.Contains(firstText(r), "copied words") {
		t.Fatalf("%+v", r)
	}
	r = call(t, cs, "desktop_click", map[string]any{"ref": "@s1:e1"})
	if !r.IsError || !strings.Contains(firstText(r), "STALE_REF") || !strings.Contains(firstText(r), "snapshot again") {
		t.Fatalf("%+v", r)
	}
}

func TestBrokerFlow(t *testing.T) {
	got := make(chan Pending, 4)
	b := NewBroker(func(p Pending) { got <- p })
	ctx, cancel := context.WithCancel(context.Background())
	run, err := b.NewRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a := HTTPApprover{URL: run.URL, Token: run.Token}
	ask := func(c Class, app string) (bool, error) {
		return a.Approve(context.Background(), Request{Class: c, Tool: "t", App: app, Summary: "s"})
	}

	// Look: asked once per app; the second time is remembered.
	res := make(chan bool, 1)
	go func() { ok, _ := ask(Look, "notepad.exe"); res <- ok }()
	p := <-got
	if p.Request.App != "notepad.exe" {
		t.Fatalf("%+v", p)
	}
	b.Respond(p.ID, Allow)
	if !<-res {
		t.Fatal("look should be allowed")
	}
	if ok, err := ask(Look, "notepad.exe"); err != nil || !ok || len(got) != 0 {
		t.Fatalf("remembered look: ok=%v err=%v asked=%d", ok, err, len(got))
	}
	// A different app asks again, and Deny is a refusal.
	go func() { ok, _ := ask(Look, "slack.exe"); res <- ok }()
	p = <-got
	b.Respond(p.ID, Deny)
	if <-res {
		t.Fatal("deny must refuse")
	}
	// Act: asked every time, until "allow all" is chosen.
	for i := 0; i < 2; i++ {
		go func() { ok, _ := ask(Act, ""); res <- ok }()
		p = <-got
		b.Respond(p.ID, Allow)
		if !<-res {
			t.Fatal("act should be allowed")
		}
	}
	go func() { ok, _ := ask(Act, ""); res <- ok }()
	p = <-got
	b.Respond(p.ID, AllowAll)
	<-res
	if ok, _ := ask(Act, ""); !ok || len(got) != 0 {
		t.Fatalf("allow-all not remembered (asked=%d)", len(got))
	}

	// A wrong token is refused; so is anything once the turn ends.
	if ok, err := (HTTPApprover{URL: run.URL, Token: "nope"}).Approve(context.Background(), Request{Class: Act}); ok || err == nil {
		t.Fatalf("bad token accepted: %v %v", ok, err)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	if ok, err := ask(Act, ""); ok || err == nil {
		t.Fatalf("ended run accepted: %v %v", ok, err)
	}
}

func TestPendingIsDeniedWhenTheTurnEnds(t *testing.T) {
	got := make(chan Pending, 1)
	b := NewBroker(func(p Pending) { got <- p })
	ctx, cancel := context.WithCancel(context.Background())
	run, _ := b.NewRun(ctx)
	res := make(chan bool, 1)
	go func() {
		ok, _ := HTTPApprover{URL: run.URL, Token: run.Token}.Approve(context.Background(), Request{Class: Act, Summary: "x"})
		res <- ok
	}()
	<-got
	cancel()
	select {
	case ok := <-res:
		if ok {
			t.Fatal("must be denied")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a waiting request was not released when the turn ended")
	}
}

func TestEnvelopeError(t *testing.T) {
	var e Envelope
	if err := json.Unmarshal([]byte(`{"ok":false,"error":{"code":"X","message":"m","suggestion":"s"}}`), &e); err != nil {
		t.Fatal(err)
	}
	if got := e.Err().Error(); got != "X: m Suggestion: s" {
		t.Fatal(got)
	}
}
func TestAutomaticRunAllowsWithoutPrompt(t *testing.T) {
	prompted := false
	broker := NewBroker(func(Pending) { prompted = true })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run, err := broker.NewAutomaticRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := (HTTPApprover{URL: run.URL, Token: run.Token}).Approve(ctx, Request{Class: Act, Tool: "test", Summary: "test action"})
	if err != nil || !allowed || prompted {
		t.Fatalf("allowed=%v prompted=%v err=%v", allowed, prompted, err)
	}
}
func TestCoordinateClickValidatesAndTargetsWindow(t *testing.T) {
	rec := &recorder{yes: true}
	cs, log := session(t, rec)
	result := call(t, cs, "desktop_click", map[string]any{"app": "notepad.exe", "window_id": "w-11", "x": 12, "y": 34})
	if result.IsError {
		t.Fatalf("coordinate click: %+v", result)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "click-at --app notepad.exe --window-id w-11 --x 12 --y 34") {
		t.Fatalf("driver args: %s", data)
	}
	result = call(t, cs, "desktop_click", map[string]any{"ref": "@s1:e1", "app": "notepad.exe", "x": 12, "y": 34})
	if !result.IsError {
		t.Fatal("accepted both ref and coordinates")
	}
}
