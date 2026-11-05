package workspace

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowTemp stops the "inside Windows" guard from rejecting a test folder when the machine's
// temp directory happens to live under the Windows folder.
func allowTemp(t *testing.T) {
	t.Helper()
	for _, e := range []string{"SystemRoot", "windir", "ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
		t.Setenv(e, `Z:\nowhere`)
	}
}

func open(t *testing.T) *Root {
	t.Helper()
	allowTemp(t)
	r, err := Open(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCleanRejectsEscapes(t *testing.T) {
	for _, bad := range []string{"../x", "a/../../x", "/etc/passwd", `C:\Windows`, "c:foo", "a/b:stream", "con", "nul.txt", "a/aux/b", "trail.", "x/" + TrashDir, "a|b", "a\x00b"} {
		if p, err := Clean(bad); err == nil {
			t.Errorf("Clean(%q) = %q, want error", bad, p)
		}
	}
	for in, want := range map[string]string{"": "", ".": "", "a//b/./c": "a/b/c", `a\b`: "a/b", " notes/todo.md ": "notes/todo.md"} {
		if got, err := Clean(in); err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestValidateLocation(t *testing.T) {
	for _, bad := range []string{"", "relative/dir", `C:\`, `C:`} {
		if ValidateLocation(bad) == nil {
			t.Errorf("ValidateLocation(%q) accepted", bad)
		}
	}
	allowTemp(t)
	t.Setenv("SystemRoot", `C:\Windows`)
	if ValidateLocation(`C:\Windows\System32\x`) == nil {
		t.Error("folder inside Windows accepted")
	}
	if ValidateLocation(`D:\Users\me\Documents\Phoenix`) != nil {
		t.Error("ordinary folder rejected")
	}
}

func TestWriteReadRoundTripAndModes(t *testing.T) {
	r := open(t)
	if _, _, err := r.Write("notes/a.md", "hello", Create); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Write("notes/a.md", "again", Create); err == nil {
		t.Fatal("create over existing file succeeded")
	}
	if _, _, err := r.Write("notes/a.md", " world", Append); err != nil {
		t.Fatal(err)
	}
	c, err := r.Read("notes/a.md", 0, 0)
	if err != nil || c.Text != "hello world" || c.Next != -1 {
		t.Fatalf("read = %+v, %v", c, err)
	}
	_, backup, err := r.Write("notes/a.md", "new", Overwrite)
	if err != nil || backup == "" {
		t.Fatalf("overwrite backup = %q, %v", backup, err)
	}
	old, err := os.ReadFile(filepath.Join(r.Dir(), filepath.FromSlash(backup)))
	if err != nil || string(old) != "hello world" {
		t.Fatalf("trashed copy = %q, %v", old, err)
	}
}

func TestReadPagesAtCharacterBoundaries(t *testing.T) {
	r := open(t)
	text := strings.Repeat("héllo✓", 50)
	if _, _, err := r.Write("u.txt", text, Create); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	var off int64
	for i := 0; i < 500; i++ {
		c, err := r.Read("u.txt", off, 7)
		if err != nil {
			t.Fatal(err)
		}
		got.WriteString(c.Text)
		if c.Next < 0 {
			break
		}
		off = c.Next
	}
	if got.String() != text {
		t.Fatal("paged reads did not reassemble the file")
	}
}

func TestBinaryAndWriteLimits(t *testing.T) {
	r := open(t)
	if err := os.WriteFile(filepath.Join(r.Dir(), "b.bin"), []byte{1, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read("b.bin", 0, 0); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("binary read error = %v", err)
	}
	if _, _, err := r.Write("big.txt", strings.Repeat("x", MaxWrite+1), Create); err == nil {
		t.Fatal("oversized write accepted")
	}
	if _, _, err := r.Write("nul.txt", "a\x00b", Create); err == nil {
		t.Fatal("NUL in text accepted")
	}
}

func TestDeleteGoesToTrashAndIsHidden(t *testing.T) {
	r := open(t)
	r.Write("keep/x.txt", "data", Create)
	to, err := r.Delete("keep")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(r.Dir(), filepath.FromSlash(to), "x.txt")); err != nil || string(b) != "data" {
		t.Fatalf("trash content = %q, %v", b, err)
	}
	es, _, _ := r.List("", 3)
	for _, e := range es {
		if strings.HasPrefix(e.Path, TrashDir) {
			t.Errorf("trash listed: %s", e.Path)
		}
	}
	if _, err := r.Delete(""); err == nil {
		t.Fatal("deleting the workspace root succeeded")
	}
	if _, err := r.Read(TrashDir+"/anything", 0, 0); err == nil {
		t.Fatal("trash readable by path")
	}
}

func TestMoveCopyNeverReplace(t *testing.T) {
	r := open(t)
	r.Write("a.txt", "A", Create)
	r.Write("b.txt", "B", Create)
	if _, _, err := r.Move("a.txt", "b.txt"); err == nil {
		t.Fatal("move replaced an existing file")
	}
	if _, _, err := r.Copy("a.txt", "b.txt"); err == nil {
		t.Fatal("copy replaced an existing file")
	}
	if _, _, err := r.Move("a.txt", "d/c.txt"); err != nil {
		t.Fatal(err)
	}
	r.Mkdir("f")
	if _, _, err := r.Move("f", "f/g"); err == nil {
		t.Fatal("folder moved into itself")
	}
	if _, _, err := r.Copy("d/c.txt", "d/c2.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestSymlinkEscapeIsRefused(t *testing.T) {
	r := open(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644)
	link := filepath.Join(r.Dir(), "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("cannot create symlinks here:", err)
	}
	if _, err := r.Read("link/secret.txt", 0, 0); err == nil {
		t.Fatal("read through a link succeeded")
	}
	if _, _, err := r.Write("link/new.txt", "x", Create); err == nil {
		t.Fatal("write through a link succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); err == nil {
		t.Fatal("file appeared outside the workspace")
	}
}

func TestSearchAndOverview(t *testing.T) {
	r := open(t)
	r.Write("notes/ideas.md", "first\nBuy Milk today\n", Create)
	r.Write("readme.txt", "nothing", Create)
	hits, _, err := r.Search("milk", "")
	if err != nil || len(hits) != 1 || hits[0].Path != "notes/ideas.md" || hits[0].Line != 2 {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
	if hits, _, _ := r.Search("readme", ""); len(hits) != 1 {
		t.Fatalf("name hits = %+v", hits)
	}
	s := r.Overview()
	if s.Files != 2 || s.Folders != 1 || !s.Complete {
		t.Fatalf("overview = %+v", s)
	}
	if d := r.Describe(); !strings.Contains(d, r.Dir()) || !strings.Contains(d, "notes/") {
		t.Fatalf("describe = %q", d)
	}
}

func TestServerToolsEndToEnd(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	ct, st := sdk.NewInMemoryTransports()
	ss, err := NewServer(r).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 8 {
		t.Fatalf("tools = %d, %v", len(tools.Tools), err)
	}
	call := func(name string, args map[string]any) (string, bool) {
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res.Content[0].(*sdk.TextContent).Text, res.IsError
	}
	if out, bad := call("files_write", map[string]any{"path": "n/a.txt", "content": "hi"}); bad {
		t.Fatal(out)
	}
	if out, bad := call("files_list", map[string]any{}); bad || !strings.Contains(out, "n/") {
		t.Fatalf("list = %q", out)
	}
	if out, bad := call("files_read", map[string]any{"path": "../escape"}); !bad {
		t.Fatalf("escape not refused: %q", out)
	}
}
