package workspace

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver: no cgo, nothing extra to ship
)

// Index is the file library's memory, kept in SQLite. The disk is the truth about what files
// exist; the index adds what the disk cannot say: what a file is for (its kind, like "resume"),
// a note, the address it was downloaded from, and who put it there. That is how the agent finds
// "the resume" again next week without being told where it went.
//
// Phoenix and the file-tool child process both open it; SQLite's WAL mode and a busy timeout
// make that safe.
type Index struct {
	db *sql.DB
}

// Meta is what the index remembers about one file.
type Meta struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	Note      string    `json:"note"`
	SourceURL string    `json:"source"`
	Origin    string    `json:"origin"` // "agent" or "user"
	Added     time.Time `json:"added"`
	Updated   time.Time `json:"updated"`
}

const schema = `
CREATE TABLE IF NOT EXISTS files (
  path       TEXT PRIMARY KEY COLLATE NOCASE,
  kind       TEXT NOT NULL DEFAULT '',
  note       TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL DEFAULT '',
  origin     TEXT NOT NULL DEFAULT 'user',
  added_at   INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS files_kind ON files(kind);
CREATE INDEX IF NOT EXISTS files_updated ON files(updated_at);
`

const (
	maxKind = 40
	maxNote = 400
	maxURL  = 1000
)

// OpenIndex opens (creating if needed) the index database at path.
func OpenIndex(path string) (*Index, error) {
	if path == "" {
		return nil, errors.New("no library database path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not prepare the library database: %w", err)
	}
	return &Index{db: db}, nil
}

// Close releases the database.
func (ix *Index) Close() error {
	if ix == nil {
		return nil
	}
	return ix.db.Close()
}

func now() int64 { return time.Now().Unix() }

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
	return clip(strings.Trim(b.String(), "-"), maxKind)
}

// Touch records that a file exists, keeping whatever kind, note and source it already had.
// origin is used only when the file is new to the index.
func (ix *Index) Touch(path, origin string) error {
	if ix == nil {
		return nil
	}
	_, err := ix.db.Exec(`INSERT INTO files(path, origin, added_at, updated_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET updated_at = excluded.updated_at`, path, origin, now(), now())
	return err
}

// Put sets a file's kind, note and source. Empty fields leave the old value alone.
func (ix *Index) Put(m Meta) error {
	if ix == nil {
		return nil
	}
	m.Kind, m.Note, m.SourceURL = CleanKind(m.Kind), clip(strings.TrimSpace(m.Note), maxNote), clip(strings.TrimSpace(m.SourceURL), maxURL)
	if m.Origin == "" {
		m.Origin = "user"
	}
	_, err := ix.db.Exec(`INSERT INTO files(path, kind, note, source_url, origin, added_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
		  kind = CASE WHEN excluded.kind = '' THEN files.kind ELSE excluded.kind END,
		  note = CASE WHEN excluded.note = '' THEN files.note ELSE excluded.note END,
		  source_url = CASE WHEN excluded.source_url = '' THEN files.source_url ELSE excluded.source_url END,
		  updated_at = excluded.updated_at`, m.Path, m.Kind, m.Note, m.SourceURL, m.Origin, now(), now())
	return err
}

// SetInfo replaces a file's kind and note outright, so either can be cleared.
func (ix *Index) SetInfo(path, kind, note string) error {
	if ix == nil {
		return nil
	}
	_, err := ix.db.Exec(`INSERT INTO files(path, kind, note, origin, added_at, updated_at) VALUES(?, ?, ?, 'user', ?, ?)
		ON CONFLICT(path) DO UPDATE SET kind = excluded.kind, note = excluded.note, updated_at = excluded.updated_at`,
		path, CleanKind(kind), clip(strings.TrimSpace(note), maxNote), now(), now())
	return err
}

const cols = `path, kind, note, source_url, origin, added_at, updated_at`

func scan(rows *sql.Rows) ([]Meta, error) {
	defer rows.Close()
	var out []Meta
	for rows.Next() {
		var m Meta
		var a, u int64
		if err := rows.Scan(&m.Path, &m.Kind, &m.Note, &m.SourceURL, &m.Origin, &a, &u); err != nil {
			return nil, err
		}
		m.Added, m.Updated = time.Unix(a, 0).UTC(), time.Unix(u, 0).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// Get returns one file's record.
func (ix *Index) Get(path string) (Meta, bool) {
	if ix == nil {
		return Meta{}, false
	}
	rows, err := ix.db.Query(`SELECT `+cols+` FROM files WHERE path = ?`, path)
	if err != nil {
		return Meta{}, false
	}
	ms, _ := scan(rows)
	if len(ms) == 0 {
		return Meta{}, false
	}
	return ms[0], true
}

// All returns every record, newest first.
func (ix *Index) All() []Meta {
	if ix == nil {
		return nil
	}
	rows, err := ix.db.Query(`SELECT ` + cols + ` FROM files ORDER BY updated_at DESC, path`)
	if err != nil {
		return nil
	}
	ms, _ := scan(rows)
	return ms
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Find searches paths, kinds, notes and source addresses. kind, when given, must match exactly.
func (ix *Index) Find(query, kind string, limit int) []Meta {
	if ix == nil {
		return nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	q := `SELECT ` + cols + ` FROM files WHERE 1=1`
	var args []any
	if kind = CleanKind(kind); kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	for _, w := range strings.Fields(query) {
		p := "%" + likeEscape(w) + "%"
		q += ` AND (path LIKE ? ESCAPE '\' OR kind LIKE ? ESCAPE '\' OR note LIKE ? ESCAPE '\' OR source_url LIKE ? ESCAPE '\')`
		args = append(args, p, p, p, p)
	}
	q += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := ix.db.Query(q, args...)
	if err != nil {
		return nil
	}
	ms, _ := scan(rows)
	return ms
}

// Move re-points records after a rename or move; a folder moves everything inside it.
func (ix *Index) Move(from, to string) error {
	if ix == nil {
		return nil
	}
	_, err := ix.db.Exec(`UPDATE files SET path = ? || substr(path, ?), updated_at = ?
		WHERE path = ? OR substr(path, 1, ?) = ? || '/'`,
		to, len(from)+1, now(), from, len(from)+1, from)
	return err
}

// Copy gives a copied file the same kind and note as its original.
func (ix *Index) Copy(from, to, origin string) error {
	if ix == nil {
		return nil
	}
	m, ok := ix.Get(from)
	if !ok {
		return ix.Touch(to, origin)
	}
	m.Path, m.Origin = to, origin
	return ix.Put(m)
}

// Remove forgets a file, or everything under a folder.
func (ix *Index) Remove(path string) error {
	if ix == nil {
		return nil
	}
	_, err := ix.db.Exec(`DELETE FROM files WHERE path = ? OR substr(path, 1, ?) = ? || '/'`, path, len(path)+1, path)
	return err
}

// Sync makes the index match the disk: files that are gone are forgotten, files the index has
// not seen (added in Explorer, say) are recorded as the user's, with a kind guessed from the
// name when it plainly says what the file is.
func (r *Root) Sync() {
	if r.idx == nil {
		return
	}
	seen := map[string]bool{}
	visited := 0
	complete := true
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
		if d.IsDir() {
			if strings.EqualFold(d.Name(), TrashDir) && filepath.Dir(p) == r.dir {
				return filepath.SkipDir
			}
			return nil
		}
		visited++
		if visited > maxWalk {
			complete = false
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(r.dir, p)
		if rerr != nil {
			return nil
		}
		seen[strings.ToLower(filepath.ToSlash(rel))] = true
		return nil
	})
	known := r.idx.All()
	have := map[string]bool{}
	for _, m := range known {
		have[strings.ToLower(m.Path)] = true
		if complete && !seen[strings.ToLower(m.Path)] {
			_ = r.idx.Remove(m.Path)
		}
	}
	for low := range seen {
		if have[low] {
			continue
		}
		// seen holds lower-cased paths; recover the real spelling from the disk.
		if rel, ok := r.realCase(low); ok {
			_ = r.idx.Put(Meta{Path: rel, Kind: GuessKind(rel), Origin: "user"})
		}
	}
}

func (r *Root) realCase(lower string) (string, bool) {
	cur := r.dir
	parts := strings.Split(lower, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		ents, err := os.ReadDir(cur)
		if err != nil {
			return "", false
		}
		found := false
		for _, e := range ents {
			if strings.ToLower(e.Name()) == part {
				out = append(out, e.Name())
				cur = filepath.Join(cur, e.Name())
				found = true
				break
			}
		}
		if !found {
			return "", false
		}
	}
	return strings.Join(out, "/"), true
}

// GuessKind labels a file by its name when the name is plain about it. Anything else is left
// unlabelled: a wrong label is worse than none.
func GuessKind(path string) string {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(name, "cover") && strings.Contains(name, "letter"):
		return "cover-letter"
	case strings.Contains(name, "resume"), strings.Contains(name, "résumé"), name == "cv.pdf", strings.HasPrefix(name, "cv-"), strings.HasPrefix(name, "cv_"), strings.HasPrefix(name, "cv."):
		return "resume"
	}
	return ""
}
