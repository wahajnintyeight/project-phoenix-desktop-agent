package library

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Export copies a file or a complete folder into a user-selected disk directory.
// It is only used by the desktop export action, never exposed as an agent tool.
// Existing items get a numbered sibling instead of being replaced or merged.
func (s *Store) Export(p, directory string) (destination string, err error) {
	n, err := s.Stat(p)
	if err != nil {
		return "", err
	}
	if n.ID == 0 {
		return "", errors.New("select a file or folder to export")
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()

	// Read a consistent tree, including folders beyond the Files view's list limit.
	// Keep only metadata in memory; file contents are read one at a time below.
	// A separate WAL reader leaves the main connection available to the UI and agent
	// even when the destination is a slow external drive.
	reader, err := sql.Open("sqlite", s.dsn)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	reader.SetMaxOpenConns(1)
	tx, err := reader.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`WITH RECURSIVE tree(id, depth) AS (
		SELECT id, 0 FROM nodes WHERE id = ? AND deleted_at IS NULL
		UNION ALL
		SELECT n.id, t.depth + 1 FROM nodes n JOIN tree t ON n.parent_id = t.id
		WHERE n.deleted_at IS NULL
	)
	SELECT n.id, COALESCE(n.parent_id, 0), n.name, n.is_dir
	FROM tree t JOIN nodes n ON n.id = t.id ORDER BY t.depth, n.id`, n.ID)
	if err != nil {
		return "", err
	}
	type entry struct {
		id, parent int64
		name       string
		dir        bool
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.parent, &e.name, &e.dir); err != nil {
			rows.Close()
			return "", err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", sql.ErrNoRows
	}

	var top string
	defer func() {
		if err != nil && top != "" {
			// top was exclusively created here, and Root confines cleanup to the picker directory.
			if cleanupErr := root.RemoveAll(top); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("could not remove incomplete export %s: %w", filepath.Join(directory, top), cleanupErr))
			}
		}
	}()
	paths := make(map[int64]string, len(entries))
	for _, e := range entries {
		parent := ""
		if e.id != n.ID {
			var ok bool
			parent, ok = paths[e.parent]
			if !ok {
				return "", errors.New("the export folder tree is incomplete")
			}
		}
		name, file, createErr := createExportItem(root, parent, e.name, e.dir)
		if createErr != nil {
			return "", fmt.Errorf("could not export %s: %w", e.name, createErr)
		}
		paths[e.id] = name
		if e.id == n.ID {
			top = name
		}
		if file == nil {
			continue
		}
		var data []byte
		readErr := tx.QueryRow(`SELECT content FROM nodes WHERE id = ?`, e.id).Scan(&data)
		if readErr == nil {
			_, readErr = file.Write(data)
		}
		closeErr := file.Close()
		if writeErr := errors.Join(readErr, closeErr); writeErr != nil {
			return "", fmt.Errorf("could not export %s: %w", e.name, writeErr)
		}
	}
	return filepath.Join(directory, top), nil
}

// ExportName also handles Windows device names (CON, NUL, etc.), which are valid
// library names but cannot be ordinary files on disk.
func ExportName(name string) string {
	name = SafeName(name)
	if !filepath.IsLocal(name) {
		name = "_" + name
	}
	return name
}

func createExportItem(root *os.Root, parent, name string, dir bool) (string, *os.File, error) {
	name = ExportName(name)
	ext := ""
	if !dir {
		ext = filepath.Ext(name)
	}
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		rel := filepath.Join(parent, candidate)
		var file *os.File
		var err error
		if dir {
			err = root.Mkdir(rel, 0o700)
		} else {
			file, err = root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return rel, file, err
	}
}
