package library

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 5

func hasColumn(ctx context.Context, conn *sql.Conn, table, column string) (bool, error) {
	rows, err := conn.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// A single transaction preserves the old database if any schema step fails.
func migrateSchema(db *sql.DB) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Take the write lock before reading user_version so concurrent opens cannot race.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "ROLLBACK")
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("library version %d is newer than supported version %d", version, schemaVersion)
	}
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		return err
	}
	for _, col := range []struct{ name, definition string }{
		{"scope", `TEXT NOT NULL DEFAULT ''`},
		{"supersedes", `INTEGER NOT NULL DEFAULT 0`},
		{"pinned", `INTEGER NOT NULL DEFAULT 0`},
	} {
		exists, err := hasColumn(ctx, conn, "learned_items", col.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := conn.ExecContext(ctx, "ALTER TABLE learned_items ADD COLUMN "+col.name+" "+col.definition); err != nil {
				return err
			}
		}
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 5"); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}
