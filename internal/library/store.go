// Package library is Phoenix's file storage: a small virtual file system kept entirely inside
// one SQLite database. Nothing here is a real file or folder on the disk, so nothing shows up in
// Explorer; the files are visible in Phoenix's Files view, to the agent through its file tools,
// and to anyone who opens the database with a SQLite viewer.
//
// Paths are slash-separated and relative to the library's top ("career/resume.pdf"). Every path
// is validated here, in the process that touches the data, so a confused or hostile model cannot
// do more than the Files view can. Deleting and overwriting are recoverable: the old content
// moves to the trash instead of being destroyed.
package library

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver: no cgo, nothing extra to ship
)

const (
	MaxFile     = 100 << 20 // largest file the user can add
	MaxDownload = 25 << 20  // largest file the agent can fetch from an address
	MaxWrite    = 1 << 20   // text the agent may write in one call
	DefaultRead = 64 << 10
	MaxRead     = 256 << 10
	MaxPreview  = 3 << 20 // largest image or text sent to the preview pane

	maxPath  = 240
	maxDepth = 12
	maxName  = 120
	maxList  = 1000
	maxKind  = 40
	maxNote  = 400
	maxURL   = 1000
)

// Store is an open library.
type Store struct {
	db *sql.DB
}

// Node is a file or folder.
type Node struct {
	ID       int64     `json:"id"`
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Mime     string    `json:"mime,omitempty"`
	Kind     string    `json:"kind,omitempty"`
	Note     string    `json:"note,omitempty"`
	Source   string    `json:"source,omitempty"`
	Origin   string    `json:"origin,omitempty"` // "agent" or "user"
	Created  time.Time `json:"created"`
	Modified time.Time `json:"modified"`
	Items    int       `json:"items,omitempty"` // children, for a folder
}

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
  id         INTEGER PRIMARY KEY,
  parent_id  INTEGER REFERENCES nodes(id),
  name       TEXT    NOT NULL,
  is_dir     INTEGER NOT NULL DEFAULT 0,
  kind       TEXT    NOT NULL DEFAULT '',
  note       TEXT    NOT NULL DEFAULT '',
  source_url TEXT    NOT NULL DEFAULT '',
  origin     TEXT    NOT NULL DEFAULT 'user',
  mime       TEXT    NOT NULL DEFAULT '',
  size       INTEGER NOT NULL DEFAULT 0,
  sha256     TEXT    NOT NULL DEFAULT '',
  content    BLOB,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER,
  trash_root INTEGER NOT NULL DEFAULT 0,
  was_path   TEXT    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS nodes_name ON nodes(COALESCE(parent_id, 0), name COLLATE NOCASE) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS nodes_parent ON nodes(parent_id);
CREATE INDEX IF NOT EXISTS nodes_kind ON nodes(kind) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS nodes_trash ON nodes(deleted_at) WHERE trash_root = 1;
CREATE TABLE IF NOT EXISTS settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS memories (
  id         INTEGER PRIMARY KEY,
  kind       TEXT    NOT NULL,
  topic      TEXT    NOT NULL,
  key        TEXT    NOT NULL DEFAULT '',
  value      TEXT    NOT NULL,
  uses       INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS memories_lookup ON memories(kind, topic COLLATE NOCASE, key COLLATE NOCASE);
PRAGMA user_version = 3;
`

// Open opens (creating if needed) the library database at dbPath.
func Open(dbPath string) (*Store, error) {
	if dbPath == "" {
		return nil, errors.New("no library database path")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	dsn := dbPath + "?_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not prepare the library database: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return s.db.Close()
}

func now() int64 { return time.Now().Unix() }

// ── Paths ──────────────────────────────────────────────────────────────────

// Clean validates a library path and returns it in canonical slash form. "" is the top.
func Clean(p string) (string, error) {
	if len(p) > maxPath*2 || strings.ContainsRune(p, 0) {
		return "", errors.New("that path is too long or invalid")
	}
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" || p == "." || p == "/" {
		return "", nil
	}
	p = strings.TrimPrefix(p, "/")
	var out []string
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", errors.New("paths cannot go up with ..; everything stays inside the library")
		}
		if err := checkName(part); err != nil {
			return "", err
		}
		out = append(out, part)
	}
	if len(out) > maxDepth {
		return "", errors.New("that path is nested too deeply")
	}
	c := strings.Join(out, "/")
	if len(c) > maxPath {
		return "", errors.New("that path is too long")
	}
	return c, nil
}

func checkName(n string) error {
	if len([]rune(n)) > maxName {
		return fmt.Errorf("%q is too long for a name", clipRunes(n, 30))
	}
	for _, r := range n {
		if r < 0x20 || strings.ContainsRune(`<>:"|?*`, r) {
			return fmt.Errorf("%q has a character that is not allowed in names", n)
		}
	}
	if strings.HasSuffix(n, ".") || strings.HasSuffix(n, " ") {
		return fmt.Errorf("%q ends with a dot or space", n)
	}
	return nil
}

// SafeName turns an arbitrary file name into one Clean accepts.
func SafeName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`<>:"|?*/\`, r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	n := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if n == "" || n == "." || n == ".." {
		n = "file"
	}
	return clipRunes(n, maxName)
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func split(p string) (dir, name string) {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "", p
	}
	return p[:i], p[i+1:]
}

func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// ── Lookup ─────────────────────────────────────────────────────────────────

const nodeCols = `id, parent_id, name, is_dir, kind, note, source_url, origin, mime, size, created_at, updated_at`

type rowScanner interface{ Scan(...any) error }

func scanNode(r rowScanner) (Node, sql.NullInt64, error) {
	var n Node
	var parent sql.NullInt64
	var dir int
	var c, u int64
	if err := r.Scan(&n.ID, &parent, &n.Name, &dir, &n.Kind, &n.Note, &n.Source, &n.Origin, &n.Mime, &n.Size, &c, &u); err != nil {
		return n, parent, err
	}
	n.Dir = dir == 1
	n.Created, n.Modified = time.Unix(c, 0).UTC(), time.Unix(u, 0).UTC()
	return n, parent, nil
}

// lookup finds the live node at a cleaned path. The top is id 0 with ok = true.
func (s *Store) lookup(clean string) (Node, bool) {
	if clean == "" {
		return Node{Dir: true}, true
	}
	var parent int64
	var n Node
	for _, part := range strings.Split(clean, "/") {
		row := s.db.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE COALESCE(parent_id,0) = ? AND name = ? COLLATE NOCASE AND deleted_at IS NULL`, parent, part)
		var err error
		n, _, err = scanNode(row)
		if err != nil {
			return Node{}, false
		}
		parent = n.ID
	}
	n.Path = clean
	return n, true
}

// Stat returns the node at a path.
func (s *Store) Stat(p string) (Node, error) {
	clean, err := Clean(p)
	if err != nil {
		return Node{}, err
	}
	n, ok := s.lookup(clean)
	if !ok {
		return Node{}, fmt.Errorf("%s does not exist", clean)
	}
	n.Path = clean
	if clean == "" {
		n.Name = ""
	}
	return n, nil
}

func (s *Store) pathOf(id int64) string {
	var parts []string
	for id != 0 && len(parts) < maxDepth+2 {
		var name string
		var parent sql.NullInt64
		if err := s.db.QueryRow(`SELECT name, parent_id FROM nodes WHERE id = ?`, id).Scan(&name, &parent); err != nil {
			break
		}
		parts = append([]string{name}, parts...)
		id = parent.Int64
	}
	return strings.Join(parts, "/")
}

// List returns the live children of a folder: folders first, then by name.
func (s *Store) List(p string) ([]Node, bool, error) {
	clean, err := Clean(p)
	if err != nil {
		return nil, false, err
	}
	dir, ok := s.lookup(clean)
	if !ok {
		return nil, false, fmt.Errorf("%s does not exist", clean)
	}
	if !dir.Dir {
		return nil, false, fmt.Errorf("%s is a file, not a folder", clean)
	}
	rows, err := s.db.Query(`SELECT `+nodeCols+`,
		(SELECT COUNT(*) FROM nodes c WHERE c.parent_id = nodes.id AND c.deleted_at IS NULL)
		FROM nodes WHERE COALESCE(parent_id,0) = ? AND deleted_at IS NULL
		ORDER BY is_dir DESC, name COLLATE NOCASE LIMIT ?`, dir.ID, maxList+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		var parent sql.NullInt64
		var isDir int
		var c, u int64
		if err := rows.Scan(&n.ID, &parent, &n.Name, &isDir, &n.Kind, &n.Note, &n.Source, &n.Origin, &n.Mime, &n.Size, &c, &u, &n.Items); err != nil {
			return nil, false, err
		}
		n.Dir = isDir == 1
		n.Path = join(clean, n.Name)
		n.Created, n.Modified = time.Unix(c, 0).UTC(), time.Unix(u, 0).UTC()
		out = append(out, n)
	}
	truncated := len(out) > maxList
	if truncated {
		out = out[:maxList]
	}
	return out, truncated, rows.Err()
}

// Folders returns every live folder path, for the navigation tree.
func (s *Store) Folders() []string {
	rows, err := s.db.Query(`SELECT id, COALESCE(parent_id,0), name FROM nodes WHERE is_dir = 1 AND deleted_at IS NULL ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type f struct {
		parent int64
		name   string
	}
	all := map[int64]f{}
	var ids []int64
	for rows.Next() {
		var id int64
		var x f
		if rows.Scan(&id, &x.parent, &x.name) == nil {
			all[id] = x
			ids = append(ids, id)
		}
	}
	var paths []string
	for _, id := range ids {
		var parts []string
		for cur, n := id, 0; cur != 0 && n <= maxDepth+1; n++ {
			x, ok := all[cur]
			if !ok {
				break
			}
			parts = append([]string{x.name}, parts...)
			cur = x.parent
		}
		paths = append(paths, strings.Join(parts, "/"))
	}
	return paths
}

// Stats counts live files and folders and the bytes they hold.
type Stats struct {
	Files   int   `json:"files"`
	Folders int   `json:"folders"`
	Bytes   int64 `json:"bytes"`
	Trash   int   `json:"trash"`
}

// Stats summarises the library.
func (s *Store) Stats() Stats {
	var st Stats
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(is_dir=0),0), COALESCE(SUM(is_dir=1),0), COALESCE(SUM(size),0) FROM nodes WHERE deleted_at IS NULL`).Scan(&st.Files, &st.Folders, &st.Bytes)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE trash_root = 1 AND deleted_at IS NOT NULL`).Scan(&st.Trash)
	return st
}

// ── Creating ───────────────────────────────────────────────────────────────

// mkdirs makes every folder on the way and returns the id of the last. It fails if a file is in
// the way.
func (s *Store) mkdirs(clean string) (int64, error) {
	var parent int64
	if clean == "" {
		return 0, nil
	}
	cur := ""
	for _, part := range strings.Split(clean, "/") {
		cur = join(cur, part)
		n, ok := s.lookup(cur)
		if ok {
			if !n.Dir {
				return 0, fmt.Errorf("%s is a file, so it cannot hold other files", cur)
			}
			parent = n.ID
			continue
		}
		id, err := s.insert(parent, part, true, nil, "", "", "", "user")
		if err != nil {
			return 0, err
		}
		parent = id
	}
	return parent, nil
}

func (s *Store) insert(parent int64, name string, dir bool, content []byte, mime, kind, source, origin string) (int64, error) {
	var par any
	if parent != 0 {
		par = parent
	}
	var blob any
	sum := ""
	if !dir {
		if content == nil {
			content = []byte{}
		}
		blob = content
		h := sha256.Sum256(content)
		sum = hex.EncodeToString(h[:])
	}
	d := 0
	if dir {
		d = 1
	}
	res, err := s.db.Exec(`INSERT INTO nodes(parent_id, name, is_dir, kind, source_url, origin, mime, size, sha256, content, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, par, name, d, kind, source, origin, mime, len(content), sum, blob, now(), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Mkdir creates a folder (and any missing parents) and returns its path.
func (s *Store) Mkdir(p, origin string) (string, error) {
	clean, err := Clean(p)
	if err != nil {
		return "", err
	}
	if clean == "" {
		return "", errors.New("give a folder name")
	}
	if n, ok := s.lookup(clean); ok {
		if n.Dir {
			return clean, nil
		}
		return "", fmt.Errorf("%s already exists as a file", clean)
	}
	if _, err := s.mkdirs(clean); err != nil {
		return "", err
	}
	if origin == "agent" {
		_, _ = s.db.Exec(`UPDATE nodes SET origin = 'agent' WHERE id = ?`, func() int64 { n, _ := s.lookup(clean); return n.ID }())
	}
	return clean, nil
}

// WriteMode says what Write does when the file already exists.
type WriteMode string

const (
	Create    WriteMode = "create"    // fail if it exists
	Overwrite WriteMode = "overwrite" // replace it; the old version goes to the trash
	Append    WriteMode = "append"    // add to the end
)

// Write saves text to a file, creating folders as needed. It returns the final size.
func (s *Store) Write(p, content string, mode WriteMode, origin string) (int64, bool, error) {
	if len(content) > MaxWrite {
		return 0, false, fmt.Errorf("that is more than %d KB in one call; write it in parts with mode \"append\"", MaxWrite>>10)
	}
	clean, err := Clean(p)
	if err != nil {
		return 0, false, err
	}
	if clean == "" {
		return 0, false, errors.New("give a file name")
	}
	n, exists := s.lookup(clean)
	if exists && n.Dir {
		return 0, false, fmt.Errorf("%s is a folder", clean)
	}
	switch mode {
	case Create, "":
		if exists {
			return 0, false, fmt.Errorf("%s already exists; use mode \"overwrite\" to replace it (the old version is kept in the trash) or \"append\"", clean)
		}
	case Overwrite, Append:
	default:
		return 0, false, errors.New(`mode must be "create", "overwrite" or "append"`)
	}
	data := []byte(content)
	if exists && mode == Append {
		old, err := s.content(n.ID)
		if err != nil {
			return 0, false, err
		}
		if int64(len(old))+int64(len(data)) > MaxFile {
			return 0, false, errors.New("the file would become too large")
		}
		data = append(old, data...)
	}
	backedUp := false
	if exists && mode == Overwrite {
		if err := s.backup(n); err != nil {
			return 0, false, err
		}
		backedUp = true
	}
	dir, name := split(clean)
	if exists {
		h := sha256.Sum256(data)
		_, err = s.db.Exec(`UPDATE nodes SET content = ?, size = ?, sha256 = ?, mime = ?, updated_at = ? WHERE id = ?`,
			data, len(data), hex.EncodeToString(h[:]), mimeFor(name, data), now(), n.ID)
		return int64(len(data)), backedUp, err
	}
	parent, err := s.mkdirs(dir)
	if err != nil {
		return 0, false, err
	}
	_, err = s.insert(parent, name, false, data, mimeFor(name, data), GuessKind(name), "", origin)
	return int64(len(data)), false, err
}

// backup keeps the current version of a file in the trash, under the same name.
func (s *Store) backup(n Node) error {
	_, err := s.db.Exec(`INSERT INTO nodes(parent_id, name, is_dir, kind, note, source_url, origin, mime, size, sha256, content, created_at, updated_at, deleted_at, trash_root, was_path)
		SELECT parent_id, name, 0, kind, note, source_url, origin, mime, size, sha256, content, created_at, updated_at, ?, 1, ? FROM nodes WHERE id = ?`,
		now(), s.pathOf(n.ID), n.ID)
	return err
}

func (s *Store) content(id int64) ([]byte, error) {
	var b []byte
	if err := s.db.QueryRow(`SELECT content FROM nodes WHERE id = ?`, id).Scan(&b); err != nil {
		return nil, err
	}
	return b, nil
}

// Put stores uploaded bytes under dir with a safe, never-clashing name and returns its path.
func (s *Store) Put(dir, name string, data []byte, origin, source string) (string, error) {
	if len(data) > MaxFile {
		return "", fmt.Errorf("%s is larger than %d MB", name, MaxFile>>20)
	}
	cdir, err := Clean(dir)
	if err != nil {
		return "", err
	}
	parent, err := s.mkdirs(cdir)
	if err != nil {
		return "", err
	}
	name = SafeName(name)
	final := s.uniqueIn(parent, name)
	id, err := s.insert(parent, final, false, data, mimeFor(name, data), GuessKind(name), clipRunes(source, maxURL), origin)
	if err != nil {
		return "", err
	}
	return s.pathOf(id), nil
}

// uniqueIn returns name, or "name (2).ext" and so on, so nothing is replaced.
func (s *Store) uniqueIn(parent int64, name string) string {
	taken := func(n string) bool {
		var one int
		return s.db.QueryRow(`SELECT 1 FROM nodes WHERE COALESCE(parent_id,0) = ? AND name = ? COLLATE NOCASE AND deleted_at IS NULL`, parent, n).Scan(&one) == nil
	}
	if !taken(name) {
		return name
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 1000; i++ {
		c := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !taken(c) {
			return c
		}
	}
	return fmt.Sprintf("%s (%d)%s", stem, time.Now().UnixNano(), ext)
}

// ── Reading ────────────────────────────────────────────────────────────────

// Bytes returns a file's whole content (for opening, previewing and exporting).
func (s *Store) Bytes(p string) (Node, []byte, error) {
	n, err := s.Stat(p)
	if err != nil {
		return n, nil, err
	}
	if n.Dir {
		return n, nil, fmt.Errorf("%s is a folder", n.Path)
	}
	b, err := s.content(n.ID)
	return n, b, err
}

// ── Info ───────────────────────────────────────────────────────────────────

// CleanKind normalises a kind label: lower case, letters digits and dashes.
func CleanKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == ' ':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	return clipRunes(strings.Trim(b.String(), "-"), maxKind)
}

// SetInfo sets a file's kind and note outright, so either can be cleared.
func (s *Store) SetInfo(p, kind, note string) error {
	n, err := s.Stat(p)
	if err != nil {
		return err
	}
	if n.ID == 0 {
		return errors.New("choose a file or folder")
	}
	_, err = s.db.Exec(`UPDATE nodes SET kind = ?, note = ?, updated_at = ? WHERE id = ?`, CleanKind(kind), clipRunes(strings.TrimSpace(note), maxNote), now(), n.ID)
	return err
}

// Annotate fills in only the fields given, leaving the others alone (for the agent).
func (s *Store) Annotate(p, kind, note, source string) error {
	n, err := s.Stat(p)
	if err != nil {
		return err
	}
	if n.ID == 0 {
		return errors.New("choose a file or folder")
	}
	if kind = CleanKind(kind); kind == "" {
		kind = n.Kind
	}
	if note = clipRunes(strings.TrimSpace(note), maxNote); note == "" {
		note = n.Note
	}
	if source == "" {
		source = n.Source
	}
	_, err = s.db.Exec(`UPDATE nodes SET kind = ?, note = ?, source_url = ?, updated_at = ? WHERE id = ?`, kind, note, clipRunes(source, maxURL), now(), n.ID)
	return err
}

// GuessKind labels a file by its name when the name is plain about it. Anything else is left
// unlabelled: a wrong label is worse than none.
func GuessKind(name string) string {
	n := strings.ToLower(path.Base(name))
	switch {
	case strings.Contains(n, "cover") && strings.Contains(n, "letter"):
		return "cover-letter"
	case strings.Contains(n, "resume"), strings.Contains(n, "résumé"), strings.HasPrefix(n, "cv."), strings.HasPrefix(n, "cv-"), strings.HasPrefix(n, "cv_"), strings.HasSuffix(strings.TrimSuffix(n, path.Ext(n)), " cv"), strings.HasSuffix(strings.TrimSuffix(n, path.Ext(n)), "_cv"):
		return "resume"
	}
	return ""
}

// ── Settings ───────────────────────────────────────────────────────────────

// Setting returns a stored value, or "" when there is none. Settings are small private notes the
// app keeps (such as the user's profile); the file tools never see them.
func (s *Store) Setting(key string) string {
	var v string
	if s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v) != nil {
		return ""
	}
	return v
}

// SetSetting stores a value; an empty value removes the setting.
func (s *Store) SetSetting(key, value string) error {
	if value == "" {
		_, err := s.db.Exec(`DELETE FROM settings WHERE key = ?`, key)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO settings(key, value, updated_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, now())
	return err
}
