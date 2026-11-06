package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"phoenix/internal/library"
)

// The Files view: a file explorer over the library, which lives inside phoenix's SQLite database
// and nowhere on disk. Every path the UI sends goes through library.Store, which validates it, so
// the view has no more reach than the agent's file tools. The only files that ever touch the disk
// are the temporary copy made when the user opens a file in another program (cleared at start-up
// and exit) and the copy the user chooses to save elsewhere.
//
// UI → Go:  f nav <loc> <arg>          loc: dir | recent | kind | trash | search
//           f refresh
//           f mkdir <path> | rename <path> <name> | move <dir> <path>... | copy <dir> <path>...
//           f delete <path>... | restore <id>... | purge <id> | empty-trash
//           f info <path> <kind> <note> | preview <path> | open <path> | export <path>...
//           f upload <dir> | put <dir> <name> <seq> <last> <base64>
// Go → UI:  u files-view data <json>  |  u files-view preview <json>

const filesViewID = "files-view"

type filesData struct {
	Loc       string              `json:"loc"` // dir | recent | kind | trash | search
	Dir       string              `json:"dir"`
	Kind      string              `json:"kind,omitempty"`
	Query     string              `json:"query,omitempty"`
	Entries   []library.Node      `json:"entries"`
	TrashList []library.TrashItem `json:"trashList,omitempty"`
	Folders   []string            `json:"folders"`
	Kinds     []library.KindCount `json:"kinds"`
	Stats     library.Stats       `json:"stats"`
	Truncated bool                `json:"truncated,omitempty"`
	Exporting bool                `json:"exporting,omitempty"`
	Message   string              `json:"message,omitempty"`
	Error     bool                `json:"error,omitempty"`
	Undo      string              `json:"undo,omitempty"` // trash ids (comma separated) that restore what was just deleted
	Select    []string            `json:"select,omitempty"`
	Seq       int                 `json:"seq"`
}

type filePreview struct {
	Node   library.Node `json:"node"`
	Image  string       `json:"image,omitempty"` // data URL
	Text   string       `json:"text,omitempty"`
	More   bool         `json:"more,omitempty"` // the text was cut short
	NoOpen bool         `json:"noOpen,omitempty"`
	Note   string       `json:"msg,omitempty"`
	Seq    int          `json:"seq"`
}

var filesState struct {
	loc       string
	dir       string
	kind      string
	query     string
	seq       int
	exporting bool
	uploads   map[string]*bytes.Buffer
}

// openFiles shows the Files tab, creating it the first time.
func openFiles() {
	for _, t := range tabs {
		if t.kind == kindFiles {
			activate(t.id)
			pushFiles("", false, "", nil)
			return
		}
	}
	t := &tab{id: nextID, kind: kindFiles, title: "Files"}
	nextID++
	tabs = append(tabs, t)
	activate(t.id)
	pushFiles("", false, "", nil)
}

// pushFiles sends the current location, the folder tree, the counts, and an optional message.
// sel names items the view should select once it has drawn them (the result of a paste, say).
func pushFiles(message string, isErr bool, undo string, sel []string) {
	filesState.seq++
	d := filesData{Loc: filesState.loc, Dir: filesState.dir, Kind: filesState.kind, Query: filesState.query,
		Message: message, Error: isErr, Undo: undo, Select: sel, Seq: filesState.seq,
		Exporting: filesState.exporting,
		Entries:   []library.Node{}, Folders: []string{}, Kinds: []library.KindCount{}}
	if d.Loc == "" {
		d.Loc = "dir"
	}
	lib, err := openLibrary()
	if err != nil {
		d.Error, d.Message = true, "The file library could not be opened: "+err.Error()
		post("u" + fs + filesViewID + fs + "data" + fs + jsonString(d))
		return
	}
	d.Folders = append(d.Folders, lib.Folders()...)
	d.Kinds = append(d.Kinds, lib.Kinds()...)
	d.Stats = lib.Stats()
	switch d.Loc {
	case "trash":
		d.TrashList = lib.TrashItems()
	case "recent":
		d.Entries = append(d.Entries, lib.Recent(100)...)
	case "kind":
		if hits, err := lib.Search("", filesState.kind, 500); err == nil {
			for _, h := range hits {
				d.Entries = append(d.Entries, h.Node)
			}
		}
	case "search":
		if hits, err := lib.Search(filesState.query, "", 200); err == nil {
			for _, h := range hits {
				d.Entries = append(d.Entries, h.Node)
			}
		}
	default:
		es, trunc, lerr := lib.List(filesState.dir)
		if lerr != nil {
			filesState.dir, d.Dir = "", ""
			d.Error, d.Message = true, lerr.Error()
			es, trunc, _ = lib.List("")
		}
		d.Entries = append(d.Entries, es...)
		d.Truncated = trunc
	}
	if d.Entries == nil {
		d.Entries = []library.Node{}
	}
	post("u" + fs + filesViewID + fs + "data" + fs + jsonString(d))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// freeName finds a name in dir that is not taken, adding " (copy)", " (copy 2)"…
func freeName(lib *library.Store, dir, name string) string {
	try := func(n string) string { return path.Join(dir, n) }
	if _, err := lib.Stat(try(name)); err != nil {
		return try(name)
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 200; i++ {
		suffix := " (copy)"
		if i > 1 {
			suffix = " (copy " + strconv.Itoa(i) + ")"
		}
		if _, err := lib.Stat(try(base + suffix + ext)); err != nil {
			return try(base + suffix + ext)
		}
	}
	return try(name)
}

func handleFilesOp(f []string) {
	if len(f) < 2 {
		return
	}
	arg := func(i int) string {
		if i < len(f) {
			return f[i]
		}
		return ""
	}
	lib, err := openLibrary()
	if err != nil {
		pushFiles(err.Error(), true, "", nil)
		return
	}
	switch f[1] {
	case "nav":
		filesState.loc, filesState.dir, filesState.kind, filesState.query = arg(2), "", "", ""
		switch filesState.loc {
		case "dir":
			filesState.dir = arg(3)
		case "kind":
			filesState.kind = arg(3)
		case "search":
			filesState.query = strings.TrimSpace(arg(3))
			if filesState.query == "" {
				filesState.loc = "dir"
			}
		case "recent", "trash":
		default:
			filesState.loc = "dir"
		}
		pushFiles("", false, "", nil)
	case "refresh":
		pushFiles("", false, "", nil)
	case "mkdir":
		p, err := lib.Mkdir(arg(2), "user")
		if err != nil {
			pushFiles(err.Error(), true, "", nil)
			return
		}
		pushFiles("Created folder "+path.Base(p), false, "", []string{p})
	case "rename":
		from := arg(2)
		to := path.Join(path.Dir(from), strings.TrimSpace(arg(3)))
		if path.Dir(from) == "." {
			to = strings.TrimSpace(arg(3))
		}
		if _, to, err := lib.Move(from, to); err != nil {
			pushFiles(err.Error(), true, "", nil)
		} else {
			pushFiles("Renamed to "+path.Base(to), false, "", []string{to})
		}
	case "move", "copy":
		dir := arg(2)
		var done []string
		var failed string
		for _, p := range f[3:] {
			if p == "" {
				continue
			}
			var to string
			var err error
			if f[1] == "move" {
				to = path.Join(dir, path.Base(p))
				_, _, err = lib.Move(p, to)
			} else {
				to = freeName(lib, dir, path.Base(p))
				_, _, err = lib.Copy(p, to, "user")
			}
			if err != nil {
				failed = err.Error()
				continue
			}
			done = append(done, to)
		}
		verb := "Moved"
		if f[1] == "copy" {
			verb = "Copied"
		}
		where := dir
		if where == "" {
			where = "the top"
		}
		switch {
		case failed != "" && len(done) == 0:
			pushFiles(failed, true, "", nil)
		case failed != "":
			pushFiles(verb+" "+plural(len(done), "item", "items")+"; one could not be done: "+failed, true, "", done)
		default:
			pushFiles(verb+" "+plural(len(done), "item", "items")+" to "+where, false, "", done)
		}
	case "delete":
		var ids []string
		for _, p := range f[2:] {
			if id, err := lib.Delete(p); err == nil {
				ids = append(ids, strconv.FormatInt(id, 10))
			}
		}
		if len(ids) == 0 {
			pushFiles("Nothing was deleted", true, "", nil)
			return
		}
		pushFiles("Moved "+plural(len(ids), "item", "items")+" to the trash", false, strings.Join(ids, ","), nil)
	case "restore":
		var back []string
		for _, s := range f[2:] {
			for _, one := range strings.Split(s, ",") {
				if id, err := strconv.ParseInt(one, 10, 64); err == nil {
					if p, err := lib.Restore(id); err == nil {
						back = append(back, p)
					}
				}
			}
		}
		if len(back) == 0 {
			pushFiles("That is no longer in the trash", true, "", nil)
			return
		}
		pushFiles("Restored "+plural(len(back), "item", "items"), false, "", back)
	case "purge":
		if id, err := strconv.ParseInt(arg(2), 10, 64); err == nil {
			_ = lib.PurgeTrash(id)
		}
		pushFiles("Deleted for good", false, "", nil)
	case "empty-trash":
		if err := lib.EmptyTrash(); err != nil {
			pushFiles(err.Error(), true, "", nil)
			return
		}
		pushFiles("Trash emptied", false, "", nil)
	case "info":
		if err := lib.SetInfo(arg(2), arg(3), arg(4)); err != nil {
			pushFiles("Could not save that: "+err.Error(), true, "", nil)
			return
		}
		pushFiles("Saved", false, "", []string{arg(2)})
		sendPreview(lib, arg(2))
	case "preview":
		sendPreview(lib, arg(2))
	case "open":
		openFromLibrary(lib, arg(2))
	case "export", "saveas":
		exportFromLibrary(lib, f[2:])
	case "upload":
		dir := arg(2)
		go func() {
			files, err := app.Dialog.OpenFile().SetTitle("Add files to Phoenix").CanChooseFiles(true).CanChooseDirectories(false).AttachToWindow(win).PromptForMultipleSelection()
			if err != nil || len(files) == 0 {
				return
			}
			var added []string
			var failed int
			var last string
			for _, src := range files {
				st, serr := os.Stat(src)
				if serr != nil || st.IsDir() {
					failed++
					last = "that is not a file"
					continue
				}
				if st.Size() > library.MaxFile {
					failed++
					last = filepath.Base(src) + " is larger than " + strconv.Itoa(library.MaxFile>>20) + " MB"
					continue
				}
				data, rerr := os.ReadFile(src)
				if rerr != nil {
					failed++
					last = rerr.Error()
					continue
				}
				if p, perr := lib.Put(dir, filepath.Base(src), data, "user", ""); perr != nil {
					failed++
					last = perr.Error()
				} else {
					added = append(added, p)
				}
			}
			runLater(func() {
				if failed > 0 {
					pushFiles(plural(len(added), "file", "files")+" added, "+strconv.Itoa(failed)+" failed: "+last, true, "", added)
					return
				}
				pushFiles("Added "+plural(len(added), "file", "files"), false, "", added)
			})
		}()
	case "put": // a file dropped on the view, sent in chunks: dir name seq last base64
		receiveUpload(lib, arg(2), arg(3), arg(4), arg(5) == "1", arg(6))
	}
}

// receiveUpload assembles a dropped file from base64 chunks and stores it when the last arrives.
func receiveUpload(lib *library.Store, dir, name, seq string, last bool, b64 string) {
	key := dir + "\x00" + name
	if filesState.uploads == nil {
		filesState.uploads = map[string]*bytes.Buffer{}
	}
	if seq == "0" || filesState.uploads[key] == nil {
		filesState.uploads[key] = &bytes.Buffer{}
	}
	buf := filesState.uploads[key]
	chunk, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || buf.Len()+len(chunk) > library.MaxFile {
		delete(filesState.uploads, key)
		pushFiles("Could not add "+name+": too large or damaged", true, "", nil)
		return
	}
	buf.Write(chunk)
	if !last {
		return
	}
	data := buf.Bytes()
	delete(filesState.uploads, key)
	saved, err := lib.Put(dir, name, data, "user", "")
	if err != nil {
		pushFiles(err.Error(), true, "", nil)
		return
	}
	pushFiles("Added "+path.Base(saved), false, "", []string{saved})
}

// sendPreview sends what the details pane shows for one file: its record, and an image or the
// start of its text when it has one.
func sendPreview(lib *library.Store, p string) {
	filesState.seq++
	pv := filePreview{Seq: filesState.seq}
	n, err := lib.Stat(p)
	if err != nil {
		pv.Note = err.Error()
		post("u" + fs + filesViewID + fs + "preview" + fs + jsonString(pv))
		return
	}
	pv.Node, pv.NoOpen = n, library.Dangerous(n.Name)
	if !n.Dir && n.Size > 0 && n.Size <= library.MaxPreview {
		if _, data, err := lib.Bytes(p); err == nil {
			mime := n.Mime
			switch {
			case strings.HasPrefix(mime, "image/") && mime != "image/svg+xml":
				pv.Image = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
			case library.IsText(data):
				if len(data) > 100<<10 {
					data, pv.More = data[:100<<10], true
				}
				pv.Text = strings.ToValidUTF8(string(data), "�")
			}
		}
	}
	post("u" + fs + filesViewID + fs + "preview" + fs + jsonString(pv))
}

// openDir is where a file is placed for another program to open. It is emptied when Phoenix
// starts and when it exits, so a copy never outlives the session.
func openDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "Phoenix", "open")
}

func clearOpenDir() { _ = os.RemoveAll(openDir()) }

func openFromLibrary(lib *library.Store, p string) {
	n, data, err := lib.Bytes(p)
	if err != nil || n.Dir {
		pushFiles("Could not open that", true, "", nil)
		return
	}
	if library.Dangerous(n.Name) {
		pushFiles("Phoenix will not launch programs or scripts. Use Export to keep a copy.", true, "", nil)
		return
	}
	dir := filepath.Join(openDir(), strconv.FormatInt(n.ID, 10)+"-"+strconv.FormatInt(time.Now().UnixNano()%1e6, 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		pushFiles("Could not prepare that file: "+err.Error(), true, "", nil)
		return
	}
	tmp := filepath.Join(dir, library.SafeName(n.Name))
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		pushFiles("Could not prepare that file: "+err.Error(), true, "", nil)
		return
	}
	// rundll32 hands the file to its default app and never runs a command line.
	_ = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", tmp).Start()
}

// showBrowser returns to the most recent browser tab (or Home) from the Files or Settings views.
func showBrowser() {
	for i := len(tabs) - 1; i >= 0; i-- {
		if t := tabs[i]; t.kind == kindWeb || t.kind == kindHome {
			activate(t.id)
			return
		}
	}
	newTab()
}
