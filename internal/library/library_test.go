package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCleanRejects(t *testing.T) {
	for _, p := range []string{"../x", "a/../../x", "C:/x", "a:b", "a\x01b", "/../x"} {
		if _, err := Clean(p); err == nil {
			t.Errorf("Clean(%q) should fail", p)
		}
	}
	if got, err := Clean("/a//b/"); err != nil || got != "a/b" {
		t.Errorf("Clean = %q, %v", got, err)
	}
}

func TestWriteListReadAndOverwrite(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Write("notes/todo.md", "one", Create, "agent"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Write("notes/todo.md", "two", Create, "agent"); err == nil {
		t.Fatal("create over existing should fail")
	}
	if _, backed, err := s.Write("notes/todo.md", "two", Overwrite, "agent"); err != nil || !backed {
		t.Fatalf("overwrite: backed=%v err=%v", backed, err)
	}
	if _, _, err := s.Write("notes/todo.md", "+three", Append, "agent"); err != nil {
		t.Fatal(err)
	}
	c, err := s.ReadText("notes/todo.md", 0, 0)
	if err != nil || c.Text != "two+three" {
		t.Fatalf("read %q %v", c.Text, err)
	}
	ns, _, err := s.List("notes")
	if err != nil || len(ns) != 1 || ns[0].Name != "todo.md" {
		t.Fatalf("list %v %v", ns, err)
	}
	if len(s.TrashItems()) != 1 {
		t.Fatalf("expected the old version in trash, got %d", len(s.TrashItems()))
	}
}

func TestMoveCopyDeleteRestore(t *testing.T) {
	s := newStore(t)
	s.Write("a/one.txt", "hello", Create, "user")
	if _, _, err := s.Copy("a", "b", "user"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Move("a/one.txt", "b/one.txt"); err == nil {
		t.Fatal("move onto existing should fail")
	}
	if _, _, err := s.Move("a/one.txt", "c/two.txt"); err != nil {
		t.Fatal(err)
	}
	id, err := s.Delete("b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat("b/one.txt"); err == nil {
		t.Fatal("deleted file still visible")
	}
	// A new item can take the name while the old one is in the trash.
	s.Mkdir("b", "user")
	if _, err := s.Restore(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat("b (restored)/one.txt"); err != nil {
		t.Fatalf("restored copy not found: %v", err)
	}
	if err := s.EmptyTrash(); err != nil {
		t.Fatal(err)
	}
	if len(s.TrashItems()) != 0 {
		t.Fatal("trash not empty")
	}
}

func TestSearchFindsByKindNoteContent(t *testing.T) {
	s := newStore(t)
	s.Put("", "cv.pdf", []byte("%PDF-1.4 binary\x00\x01"), "agent", "https://x.example/cv.pdf")
	s.Annotate("cv.pdf", "resume", "for the Acme job", "")
	s.Write("notes.txt", "remember the marmalade", Create, "agent")
	if h, _ := s.Search("", "resume", 10); len(h) != 1 || h[0].Path != "cv.pdf" {
		t.Fatalf("kind search: %v", h)
	}
	if h, _ := s.Search("acme", "", 10); len(h) != 1 {
		t.Fatalf("note search: %v", h)
	}
	if h, _ := s.Search("marmalade", "", 10); len(h) != 1 || h[0].Path != "notes.txt" {
		t.Fatalf("content search: %v", h)
	}
	if !strings.Contains(s.Describe(), "resume") {
		t.Fatalf("describe lacks kind: %q", s.Describe())
	}
}

func TestSaveURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/missing") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-fake"))
	}))
	defer srv.Close()
	allowPrivate = true
	defer func() { allowPrivate = false }()

	s := newStore(t)
	saved, size, err := s.SaveURL(context.Background(), srv.URL+"/files/My%20Resume.pdf", "", "resume", "latest")
	if err != nil || size != 9 {
		t.Fatalf("save: %q %d %v", saved, size, err)
	}
	n, err := s.Stat(saved)
	if err != nil || n.Kind != "resume" || n.Source == "" || n.Origin != "agent" {
		t.Fatalf("node %+v %v", n, err)
	}
	if _, _, err := s.SaveURL(context.Background(), srv.URL+"/files/My%20Resume.pdf", saved, "", ""); err == nil {
		t.Fatal("saving over an existing path should fail")
	}
	if _, _, err := s.SaveURL(context.Background(), srv.URL+"/missing", "", "", ""); err == nil {
		t.Fatal("404 should fail")
	}
	if _, _, err := s.SaveURL(context.Background(), srv.URL+"/x.exe", "", "", ""); err == nil {
		t.Fatal("program downloads should be refused")
	}
	allowPrivate = false
	if _, _, err := s.SaveURL(context.Background(), srv.URL+"/a.pdf", "", "", ""); err == nil {
		t.Fatal("plain http / loopback must be refused")
	}
	if _, _, err := s.SaveURL(context.Background(), "https://user:pw@example.com/a.pdf", "", "", ""); err == nil {
		t.Fatal("userinfo must be refused")
	}
}

func TestTools(t *testing.T) {
	s := newStore(t)
	srv := NewServer(s, "agent")
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 17 {
		t.Fatalf("tools: %d %v", len(tools.Tools), err)
	}
	call := func(name string, args map[string]any) (string, bool) {
		r, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return r.Content[0].(*sdk.TextContent).Text, r.IsError
	}
	if out, bad := call("files_write", map[string]any{"path": "r/cv.md", "content": "hi", "kind": "resume"}); bad {
		t.Fatal(out)
	}
	if out, _ := call("files_find", map[string]any{"kind": "resume"}); !strings.Contains(out, "r/cv.md") {
		t.Fatalf("find: %q", out)
	}
	if _, bad := call("files_read", map[string]any{"path": "../x"}); !bad {
		t.Fatal("escape should error")
	}
	if out, bad := call("files_delete", map[string]any{"path": "r/cv.md"}); bad || !strings.Contains(out, "trash") {
		t.Fatalf("delete: %q", out)
	}
}

func TestSettings(t *testing.T) {
	s := newStore(t)
	if s.Setting("a") != "" {
		t.Fatal("unset should be empty")
	}
	if err := s.SetSetting("a", "one"); err != nil {
		t.Fatal(err)
	}
	s.SetSetting("a", "two")
	if s.Setting("a") != "two" {
		t.Fatal("update failed")
	}
	s.SetSetting("a", "")
	if s.Setting("a") != "" {
		t.Fatal("clear failed")
	}
}
