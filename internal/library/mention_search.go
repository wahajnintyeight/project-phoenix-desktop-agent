package library

import (
	"errors"
	"strings"
)

// MentionSearch finds a bounded set of live names and their library paths without reading file contents.
func (s *Store) MentionSearch(query string, limit int) ([]Node, error) {
	query = strings.TrimSpace(query)
	if len(query) > 400 {
		return nil, errors.New("that search is too long")
	}
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	rows, err := s.db.Query(`WITH RECURSIVE found AS (
		SELECT id, parent_id, name, is_dir FROM nodes
		WHERE deleted_at IS NULL AND name LIKE ? ESCAPE '\'
		ORDER BY id DESC LIMIT ?
	), paths(id, parent_id, path, depth) AS (
		SELECT id, parent_id, name, 0 FROM found
		UNION ALL
		SELECT paths.id, parent.parent_id, parent.name || '/' || paths.path, paths.depth + 1
		FROM paths JOIN nodes parent ON parent.id = paths.parent_id WHERE paths.depth < ?
	)
	SELECT found.id, found.name, found.is_dir, paths.path FROM found JOIN paths ON paths.id = found.id
	WHERE paths.parent_id IS NULL ORDER BY found.id DESC`, "%"+likeEscape(query)+"%", limit, maxDepth+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Name, &n.Dir, &n.Path); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}
