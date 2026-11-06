package main

import (
	"path/filepath"
	"strings"
	"testing"

	"phoenix/internal/browser"
	"phoenix/internal/library"
)

func TestFillFormArgumentsAreBounded(t *testing.T) {
	ref := "@s12345:e1"
	ok := browser.Call{Tool: "browser_fill_form", Arguments: []byte(`{"tab_id":1,"fields":[{"ref":"` + ref + `","value":"x"}]}`)}
	if _, err := decodeBrowserCall(ok); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string]string{
		"no fields": `{"tab_id":1,"fields":[]}`,
		"bad ref":   `{"tab_id":1,"fields":[{"ref":"nope","value":"x"}]}`,
		"no tab":    `{"tab_id":0,"fields":[{"ref":"` + ref + `","value":"x"}]}`,
	} {
		if _, err := decodeBrowserCall(browser.Call{Tool: "browser_fill_form", Arguments: []byte(args)}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := decodeBrowserCall(browser.Call{Tool: "browser_upload", Arguments: []byte(`{"tab_id":1,"file":"resume"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeBrowserCall(browser.Call{Tool: "browser_upload", Arguments: []byte(`{"tab_id":1,"file":""}`)}); err == nil {
		t.Fatal("empty upload file accepted")
	}
}

func TestTruthy(t *testing.T) {
	for _, v := range []string{"true", "Yes", " on ", "checked", "1"} {
		if !truthy(v) {
			t.Errorf("%q should be true", v)
		}
	}
	for _, v := range []string{"", "false", "no", "0", "maybe"} {
		if truthy(v) {
			t.Errorf("%q should be false", v)
		}
	}
}

// "resume" means the user's newest resume, and only files from the library can be named.
func TestUploadResolvesLibraryFilesByPathNameAndKind(t *testing.T) {
	lib, err := library.Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	old, _ := lib.Put("docs", "cv-2023.pdf", []byte("%PDF-old"), "user", "")
	cur, _ := lib.Put("docs", "cv-2025.pdf", []byte("%PDF-new"), "user", "")
	_ = lib.SetInfo(old, "resume", "")
	_ = lib.SetInfo(cur, "resume", "")

	if n, _, err := resolveLibraryFile(lib, cur); err != nil || n.Path != cur {
		t.Fatalf("by path: %v %v", n.Path, err)
	}
	if n, _, err := resolveLibraryFile(lib, "cv-2023.pdf"); err != nil || n.Path != old {
		t.Fatalf("by name: %v %v", n.Path, err)
	}
	if n, data, err := resolveLibraryFile(lib, "resume"); err != nil || n.Path != cur || !strings.Contains(string(data), "new") {
		t.Fatalf("by kind should pick the newest: %v %v", n.Path, err)
	}
	if _, _, err := resolveLibraryFile(lib, "C:/Windows/system32/config/SAM"); err == nil {
		t.Fatal("an arbitrary disk path must not resolve")
	}
}

func TestSnapshotRecordsCheckedState(t *testing.T) {
	nodes := []axNode{{NodeID: "1", Role: axValue{Value: "checkbox"}, Name: axValue{Value: "I agree"}, BackendNodeID: 1,
		Properties: []axProperty{{Name: "checked", Value: axValue{Value: "true"}}}}}
	session := &browserSession{}
	if _, err := formatBrowserSnapshot(browserTabState{ID: 1}, "", nodes, session, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range session.snapshot.refs {
		if !r.Checked {
			t.Fatal("checked checkbox not recorded as checked")
		}
	}
}
