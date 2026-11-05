// Package workspace is the agent's storage: one folder the user chose, and a small set of file
// operations that can never leave it. Everything the model passes is a relative path; every
// path is validated here, in the process that touches the disk, so a confused or hostile
// model (or a page that talked it into something) cannot reach anything outside the folder.
//
// Deletes and overwrites are recoverable: the previous content moves to a trash folder inside
// the workspace instead of being destroyed. That is what makes it reasonable to let the agent
// work without a confirmation for every file it touches.
package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// TrashDir is where deleted and overwritten files go. It is inside the workspace, so the
	// user can see and restore them in Explorer, and it is hidden from the agent's listings.
	TrashDir = ".phoenix-trash"

	MaxWrite     = 1 << 20   // bytes the agent may write in one call
	DefaultRead  = 64 << 10  // bytes returned by a read when no limit is given
	MaxRead      = 256 << 10 // the most one read returns
	MaxCopy      = 50 << 20  // largest file the agent may copy
	maxPath      = 240       // characters in a relative path
	maxDepth     = 12        // directory levels below the workspace
	maxList      = 300       // entries one listing returns
	maxWalk      = 5000      // entries a search or overview will visit
	maxSearchHit = 50
	searchFileKB = 1 << 20 // text files larger than this are matched by name only
)

// Root is an open workspace.
type Root struct {
	dir    string // absolute, symlink-free, no trailing separator
	idx    *Index // optional: what each file is for
	origin string // who is acting through this Root: "agent" or "user"
}

// AttachIndex connects the SQLite index; origin names who acts through this Root.
func (r *Root) AttachIndex(ix *Index, origin string) {
	r.idx, r.origin = ix, origin
}

// Index returns the attached index, or nil.
func (r *Root) Index() *Index { return r.idx }

func (r *Root) who() string {
	if r.origin == "" {
		return "user"
	}
	return r.origin
}

// Open prepares dir as a workspace, creating it when missing. dir must be an absolute path to
// an ordinary folder: a drive root, the Windows folder, Program Files and the like are refused
// so a mistaken setting cannot hand the agent the whole machine.
func Open(dir string) (*Root, error) {
	if err := ValidateLocation(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("could not create the workspace folder: %w", err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("could not open the workspace folder: %w", err)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(real)
	if err != nil || !st.IsDir() {
		return nil, errors.New("the workspace location is not a folder")
	}
	return &Root{dir: strings.TrimRight(real, `\/`)}, nil
}

// Dir is the absolute path of the workspace, for showing to the user and to Explorer.
func (r *Root) Dir() string { return r.dir }

// ValidateLocation reports whether dir may be a workspace.
func ValidateLocation(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" || !filepath.IsAbs(dir) || strings.ContainsRune(dir, 0) {
		return errors.New("choose a full folder path, such as C:\\Users\\you\\Documents\\Phoenix")
	}
	clean := filepath.Clean(dir)
	if vol := filepath.VolumeName(clean); clean == vol+`\` || clean == vol {
		return errors.New("choose a folder, not a whole drive")
	}
	lower := strings.ToLower(clean)
	for _, env := range []string{"SystemRoot", "windir", "ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
		if p := os.Getenv(env); p != "" {
			p = strings.ToLower(filepath.Clean(p))
			if lower == p || strings.HasPrefix(lower, p+`\`) {
				return errors.New("that folder is part of Windows or installed programs; choose one in your own files")
			}
		}
	}
	return nil
}

var reservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// Clean validates a workspace-relative path and returns it in canonical slash form. "" and "."
// are the workspace itself. It rejects anything that could name a place outside the folder or a
// special Windows object.
func Clean(rel string) (string, error) {
	if len(rel) > maxPath*2 || strings.ContainsRune(rel, 0) {
		return "", errors.New("that path is too long or invalid")
	}
	rel = strings.ReplaceAll(strings.TrimSpace(rel), `\`, "/")
	if rel == "" || rel == "." || rel == "/" {
		return "", nil
	}
	if strings.HasPrefix(rel, "/") || filepath.VolumeName(rel) != "" || strings.Contains(rel, ":") {
		return "", errors.New("use a path relative to the workspace, like notes/todo.md (no drive letters or leading slashes)")
	}
	parts := strings.Split(rel, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		switch {
		case p == "" || p == ".":
			continue
		case p == "..":
			return "", errors.New("paths cannot go up with ..; everything stays inside the workspace")
		}
		for _, r := range p {
			if r < 0x20 || strings.ContainsRune(`<>:"|?*`, r) {
				return "", fmt.Errorf("%q has a character Windows does not allow in file names", p)
			}
		}
		if strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ") {
			return "", fmt.Errorf("%q ends with a dot or space, which Windows does not allow", p)
		}
		stem := strings.ToLower(p)
		if i := strings.IndexByte(stem, '.'); i >= 0 {
			stem = stem[:i]
		}
		if reservedNames[stem] {
			return "", fmt.Errorf("%q is a reserved Windows device name", p)
		}
		if strings.EqualFold(p, TrashDir) {
			return "", errors.New("that folder is Phoenix's trash and is not available to the agent")
		}
		out = append(out, p)
	}
	if len(out) > maxDepth {
		return "", errors.New("that path is nested too deeply")
	}
	clean := strings.Join(out, "/")
	if len(clean) > maxPath {
		return "", errors.New("that path is too long")
	}
	return clean, nil
}

// resolve turns a validated relative path into an absolute one and checks that no existing
// part of it is a symbolic link or junction (which could lead outside the workspace).
func (r *Root) resolve(rel string) (clean, abs string, err error) {
	clean, err = Clean(rel)
	if err != nil {
		return "", "", err
	}
	abs = r.dir
	if clean == "" {
		return clean, abs, nil
	}
	for _, p := range strings.Split(clean, "/") {
		abs = filepath.Join(abs, p)
		st, lerr := os.Lstat(abs)
		if lerr != nil {
			if errors.Is(lerr, fs.ErrNotExist) {
				continue // the rest does not exist yet; nothing there to follow
			}
			return "", "", lerr
		}
		if st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return "", "", fmt.Errorf("%q is a link, and links are not followed inside the workspace", clean)
		}
	}
	return clean, abs, nil
}

// Entry is one item in a listing.
type Entry struct {
	Path     string    `json:"path"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func entryFor(rel string, st fs.FileInfo) Entry {
	e := Entry{Path: rel, Dir: st.IsDir(), Modified: st.ModTime().UTC().Truncate(time.Second)}
	if !st.IsDir() {
		e.Size = st.Size()
	}
	return e
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Dir != es[j].Dir {
			return es[i].Dir
		}
		return strings.ToLower(es[i].Path) < strings.ToLower(es[j].Path)
	})
}

// List returns the entries of a folder, folders first. depth 1 is the folder itself, up to 3
// levels below it. The trash and links are never listed. truncated reports that the limit cut
// the listing short.
func (r *Root) List(rel string, depth int) (entries []Entry, truncated bool, err error) {
	clean, abs, err := r.resolve(rel)
	if err != nil {
		return nil, false, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, false, friendly(err, clean)
	}
	if !st.IsDir() {
		return []Entry{entryFor(clean, st)}, false, nil
	}
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	var walk func(dirRel, dirAbs string, level int)
	walk = func(dirRel, dirAbs string, level int) {
		items, rerr := os.ReadDir(dirAbs)
		if rerr != nil {
			return
		}
		var level1 []Entry
		for _, it := range items {
			name := it.Name()
			if strings.EqualFold(name, TrashDir) && dirRel == "" {
				continue
			}
			info, ierr := it.Info()
			if ierr != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				continue
			}
			p := name
			if dirRel != "" {
				p = dirRel + "/" + name
			}
			level1 = append(level1, entryFor(p, info))
		}
		sortEntries(level1)
		for _, e := range level1 {
			if len(entries) >= maxList {
				truncated = true
				return
			}
			entries = append(entries, e)
			if e.Dir && level < depth {
				walk(e.Path, filepath.Join(r.dir, filepath.FromSlash(e.Path)), level+1)
				if truncated {
					return
				}
			}
		}
	}
	walk(clean, abs, 1)
	return entries, truncated, nil
}

// Content is the result of a read.
type Content struct {
	Path   string
	Text   string
	Size   int64 // bytes in the file
	Offset int64 // where Text starts
	Next   int64 // where to continue, or -1 when the end was reached
}

// Read returns text from a file. Binary files are refused with a description, because dumping
// bytes into the model's context helps nobody.
func (r *Root) Read(rel string, offset int64, limit int) (Content, error) {
	clean, abs, err := r.resolve(rel)
	if err != nil {
		return Content{}, err
	}
	if clean == "" {
		return Content{}, errors.New("that is the workspace folder; use files_list to see what is in it")
	}
	f, err := os.Open(abs)
	if err != nil {
		return Content{}, friendly(err, clean)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Content{}, err
	}
	if st.IsDir() {
		return Content{}, fmt.Errorf("%s is a folder; use files_list", clean)
	}
	if limit <= 0 {
		limit = DefaultRead
	}
	if limit > MaxRead {
		limit = MaxRead
	}
	if offset < 0 || offset > st.Size() {
		return Content{}, fmt.Errorf("offset is outside the file (size %d bytes)", st.Size())
	}
	// Judge binary-ness from the start of the file, not from wherever the read begins.
	head := make([]byte, 4096)
	hn, _ := f.ReadAt(head, 0)
	if looksBinary(head[:hn]) {
		return Content{}, fmt.Errorf("%s looks like a binary file (%s, %d bytes); only text files can be read", clean, describeExt(clean), st.Size())
	}
	buf := make([]byte, limit)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return Content{}, err
	}
	buf = buf[:n]
	// Cut at a character boundary so the text is valid and the next read lines up.
	end := len(buf)
	for end > 0 && end < len(buf) && !utf8.RuneStart(buf[end]) {
		end--
	}
	if offset+int64(n) < st.Size() { // more follows: do not end in the middle of a character
		for end > 0 && !utf8.Valid(buf[:end]) {
			end--
		}
		buf = buf[:end]
	}
	c := Content{Path: clean, Text: string(buf), Size: st.Size(), Offset: offset, Next: offset + int64(len(buf))}
	if c.Next >= st.Size() {
		c.Next = -1
	}
	return c, nil
}

func describeExt(p string) string {
	if e := strings.ToLower(filepath.Ext(p)); e != "" {
		return e + " file"
	}
	return "no extension"
}

func looksBinary(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	// Valid UTF-8 (allowing a character cut at the end) is text; anything else is binary.
	for i := 0; i < 4 && len(b) > 0; i++ {
		if utf8.Valid(b) {
			return false
		}
		b = b[:len(b)-1]
	}
	return true
}

// WriteMode says what Write may do when the file already exists.
type WriteMode string

const (
	Create    WriteMode = "create"    // fail if it exists
	Overwrite WriteMode = "overwrite" // replace it; the old version goes to the trash
	Append    WriteMode = "append"    // add to the end
)

// Write saves text to a file, creating folders as needed. It returns the final size.
func (r *Root) Write(rel, content string, mode WriteMode) (size int64, backup string, err error) {
	clean, abs, err := r.resolve(rel)
	if err != nil {
		return 0, "", err
	}
	if clean == "" {
		return 0, "", errors.New("give a file name, like notes/todo.md")
	}
	if len(content) > MaxWrite {
		return 0, "", fmt.Errorf("that is %d bytes; write at most %d bytes per call (split it, or use append)", len(content), MaxWrite)
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return 0, "", errors.New("only text can be written")
	}
	if mode == "" {
		mode = Create
	}
	st, serr := os.Stat(abs)
	exists := serr == nil
	if exists && st.IsDir() {
		return 0, "", fmt.Errorf("%s is a folder", clean)
	}
	switch mode {
	case Create:
		if exists {
			return 0, "", fmt.Errorf("%s already exists; use mode \"overwrite\" to replace it (the old version is kept in the trash) or \"append\"", clean)
		}
	case Overwrite:
		if exists {
			if backup, err = r.toTrash(clean, abs); err != nil {
				return 0, "", err
			}
		}
	case Append:
	default:
		return 0, "", errors.New(`mode must be "create", "overwrite" or "append"`)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, "", err
	}
	// Re-check after creating folders: nothing on the way may have become a link.
	if _, _, err := r.resolve(clean); err != nil {
		return 0, "", err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if mode == Append {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(abs, flags, 0o644)
	if err != nil {
		return 0, "", friendly(err, clean)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return 0, "", err
	}
	if err := f.Close(); err != nil {
		return 0, "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return 0, backup, err
	}
	r.remember(clean)
	return fi.Size(), backup, nil
}

// Mkdir creates a folder (and any missing parents).
func (r *Root) Mkdir(rel string) (string, error) {
	clean, abs, err := r.resolve(rel)
	if err != nil {
		return "", err
	}
	if clean == "" {
		return "", errors.New("give a folder name")
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", friendly(err, clean)
	}
	if _, _, err := r.resolve(clean); err != nil {
		return "", err
	}
	return clean, nil
}

// Move renames or moves a file or folder. It never replaces an existing destination.
func (r *Root) Move(from, to string) (string, string, error) {
	fc, fa, err := r.resolve(from)
	if err != nil {
		return "", "", err
	}
	tc, ta, err := r.resolve(to)
	if err != nil {
		return "", "", err
	}
	if fc == "" || tc == "" {
		return "", "", errors.New("the workspace folder itself cannot be moved")
	}
	if _, err := os.Lstat(fa); err != nil {
		return "", "", friendly(err, fc)
	}
	if strings.EqualFold(fc, tc) {
		return "", "", errors.New("the source and destination are the same")
	}
	if strings.HasPrefix(strings.ToLower(tc), strings.ToLower(fc)+"/") {
		return "", "", errors.New("a folder cannot be moved into itself")
	}
	if _, err := os.Lstat(ta); err == nil {
		return "", "", fmt.Errorf("%s already exists; choose another name or delete it first", tc)
	}
	if err := os.MkdirAll(filepath.Dir(ta), 0o755); err != nil {
		return "", "", err
	}
	if _, _, err := r.resolve(tc); err != nil {
		return "", "", err
	}
	if err := os.Rename(fa, ta); err != nil {
		return "", "", friendly(err, fc)
	}
	_ = r.idx.Move(fc, tc)
	return fc, tc, nil
}

// Copy duplicates a file. It never replaces an existing destination.
func (r *Root) Copy(from, to string) (string, string, error) {
	fc, fa, err := r.resolve(from)
	if err != nil {
		return "", "", err
	}
	tc, ta, err := r.resolve(to)
	if err != nil {
		return "", "", err
	}
	if fc == "" || tc == "" {
		return "", "", errors.New("give file names for both")
	}
	st, err := os.Stat(fa)
	if err != nil {
		return "", "", friendly(err, fc)
	}
	if st.IsDir() {
		return "", "", errors.New("only files can be copied; create the folder and copy files into it")
	}
	if st.Size() > MaxCopy {
		return "", "", fmt.Errorf("that file is larger than %d MB", MaxCopy>>20)
	}
	if _, err := os.Lstat(ta); err == nil {
		return "", "", fmt.Errorf("%s already exists; choose another name", tc)
	}
	if err := os.MkdirAll(filepath.Dir(ta), 0o755); err != nil {
		return "", "", err
	}
	if _, _, err := r.resolve(tc); err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(fa)
	if err != nil {
		return "", "", friendly(err, fc)
	}
	if err := os.WriteFile(ta, data, 0o644); err != nil {
		return "", "", friendly(err, tc)
	}
	_ = r.idx.Copy(fc, tc, r.who())
	return fc, tc, nil
}

// Delete moves a file or folder to the trash; nothing is destroyed. It returns where it went.
func (r *Root) Delete(rel string) (string, error) {
	clean, abs, err := r.resolve(rel)
	if err != nil {
		return "", err
	}
	if clean == "" {
		return "", errors.New("the workspace folder itself cannot be deleted")
	}
	if _, err := os.Lstat(abs); err != nil {
		return "", friendly(err, clean)
	}
	to, err := r.toTrash(clean, abs)
	if err == nil {
		_ = r.idx.Remove(clean)
	}
	return to, err
}

// remember records a file the Root just created or changed.
func (r *Root) remember(clean string) {
	if r.idx == nil {
		return
	}
	if _, ok := r.idx.Get(clean); ok {
		_ = r.idx.Touch(clean, r.who())
		return
	}
	_ = r.idx.Put(Meta{Path: clean, Kind: GuessKind(clean), Origin: r.who()})
}

// toTrash moves abs to <workspace>/.phoenix-trash/<timestamp>/<rel> and returns that location
// relative to the workspace.
func (r *Root) toTrash(clean, abs string) (string, error) {
	stamp := time.Now().UTC().Format("20060102-150405.000")
	dest := filepath.Join(r.dir, TrashDir, stamp, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(abs, dest); err != nil {
		return "", friendly(err, clean)
	}
	return TrashDir + "/" + stamp + "/" + clean, nil
}

// Hit is one search result.
type Hit struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"` // 1-based, for a text match
	Text string `json:"text,omitempty"` // the matching line, clipped
}

// Search finds files whose name contains query, and lines of text files that contain it
// (case-insensitive). folder limits the search; "" searches the whole workspace.
func (r *Root) Search(query, folder string) (hits []Hit, truncated bool, err error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, false, errors.New("give something to search for")
	}
	if len(query) > 200 {
		return nil, false, errors.New("that search is too long")
	}
	clean, abs, err := r.resolve(folder)
	if err != nil {
		return nil, false, err
	}
	needle := strings.ToLower(query)
	visited := 0
	werr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && strings.EqualFold(d.Name(), TrashDir) {
			return filepath.SkipDir
		}
		visited++
		if visited > maxWalk || len(hits) >= maxSearchHit {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(r.dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if strings.Contains(strings.ToLower(d.Name()), needle) {
			hits = append(hits, Hit{Path: rel})
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() == 0 || info.Size() > searchFileKB {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil || looksBinary(data[:min(len(data), 4096)]) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				hits = append(hits, Hit{Path: rel, Line: i + 1, Text: clip(strings.TrimSpace(line), 160)})
				if len(hits) >= maxSearchHit {
					truncated = true
					return filepath.SkipAll
				}
				break // one line per file keeps results broad rather than deep
			}
		}
		return nil
	})
	_ = clean
	return hits, truncated, werr
}

// Summary describes the workspace for the agent: where it is, how big, and what is at the top.
type Summary struct {
	Dir      string
	Files    int
	Folders  int
	Bytes    int64
	Complete bool // false when the count stopped at the visit limit
	Top      []Entry
}

// Overview walks the workspace (bounded) and returns a summary.
func (r *Root) Overview() Summary {
	s := Summary{Dir: r.dir, Complete: true}
	visited := 0
	_ = filepath.WalkDir(r.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && strings.EqualFold(d.Name(), TrashDir) {
			return filepath.SkipDir
		}
		if p == r.dir {
			return nil
		}
		visited++
		if visited > maxWalk {
			s.Complete = false
			return filepath.SkipAll
		}
		if d.IsDir() {
			s.Folders++
			return nil
		}
		s.Files++
		if info, ierr := d.Info(); ierr == nil {
			s.Bytes += info.Size()
		}
		return nil
	})
	if top, _, err := r.List("", 1); err == nil {
		if len(top) > 25 {
			top = top[:25]
		}
		s.Top = top
	}
	return s
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// friendly turns an OS error into a sentence that names the workspace path, not the disk path.
func friendly(err error, rel string) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s does not exist", orRoot(rel))
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("Windows would not allow access to %s (it may be open in another program)", orRoot(rel))
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%s already exists", orRoot(rel))
	}
	return fmt.Errorf("could not use %s: %v", orRoot(rel), stripPath(err))
}

func orRoot(rel string) string {
	if rel == "" {
		return "the workspace"
	}
	return rel
}

// stripPath keeps the OS's reason but not the absolute disk path inside the message.
func stripPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
