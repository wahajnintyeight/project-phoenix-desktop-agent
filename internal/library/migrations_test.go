package library

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestSchemaUpgradeFromPreLearningDatabase(t *testing.T) {
	for _, version := range []int{2, 3} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "older.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(schema + fmt.Sprintf(`DROP TABLE learned_items; DROP TABLE learned_suppression; INSERT INTO settings(key,value,updated_at) VALUES('profile.text','keep',1); INSERT INTO memories(kind,topic,key,value,created_at,updated_at) VALUES('fact','Profile','name','Alex',1,1); PRAGMA user_version=%d;`, version))
			if err != nil {
				t.Fatal(err)
			}
			db.Close()
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if got := s.Setting("profile.text"); got != "keep" {
				t.Fatalf("profile lost: %q", got)
			}
			entries, err := s.Memories()
			if err != nil || len(entries) != 1 || entries[0].Value != "Alex" {
				t.Fatalf("memories: %+v %v", entries, err)
			}
			if _, err := s.Lessons(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSchemaMigrationPreservesLessons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + `INSERT INTO learned_items(kind,trigger_text,lesson,status,created_at,updated_at) VALUES('workflow','jobs','Read listing','active',1,1); PRAGMA user_version=3;`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		lessons, err := s.Lessons()
		if err != nil || len(lessons) != 1 || lessons[0].Lesson != "Read listing" {
			t.Fatalf("lessons: %+v %v", lessons, err)
		}
		var version int
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
			t.Fatalf("version: %d %v", version, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSchemaRejectsFutureVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("accepted future schema")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 999 {
		t.Fatalf("version: %d %v", version, err)
	}
}

func TestSchemaUpgradeFromVersionFour(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v4.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(schema + `ALTER TABLE learned_items ADD COLUMN scope TEXT NOT NULL DEFAULT ''; ALTER TABLE learned_items ADD COLUMN supersedes INTEGER NOT NULL DEFAULT 0; INSERT INTO learned_items(kind,trigger_text,lesson,status,created_at,updated_at) VALUES('workflow','jobs','Read listing','active',1,1); PRAGMA user_version=4;`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lessons, err := s.Lessons()
	if err != nil || len(lessons) != 1 || lessons[0].Pinned {
		t.Fatalf("lessons: %+v %v", lessons, err)
	}
}

func TestSchemaMigrationRollsBackOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + `DROP INDEX learned_items_unique; INSERT INTO learned_items(kind,trigger_text,lesson,created_at,updated_at) VALUES('workflow','same','duplicate',1,1),('workflow','same','duplicate',2,2); PRAGMA user_version=3;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("accepted a broken migration")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, scopeCount int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('learned_items') WHERE name='scope'`).Scan(&scopeCount); err != nil {
		t.Fatal(err)
	}
	if version != 3 || scopeCount != 0 {
		t.Fatalf("partial migration: version=%d scope=%d", version, scopeCount)
	}
}

func TestSchemaMigrationPreservesSuppressedLessons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + `PRAGMA user_version=3; INSERT INTO settings(key,value,updated_at) VALUES('learning.enabled','1',1);`); err != nil {
		t.Fatal(err)
	}
	fingerprint := lessonFingerprint("workflow", "visa sponsorship", "Check policy")
	if _, err := db.Exec(`INSERT INTO learned_suppression(fingerprint,created_at) VALUES(?,1)`, fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.ProposeLesson("workflow", "visa sponsorship", "Check policy", "fixture", ""); err == nil {
		t.Fatal("migration lost the forgotten-lesson suppression")
	}
}

func TestSchemaMigrationResumesWhenScopeAlreadyExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(schema + `ALTER TABLE learned_items ADD COLUMN scope TEXT NOT NULL DEFAULT ''; INSERT INTO learned_items(kind,trigger_text,lesson,status,created_at,updated_at,scope) VALUES('workflow','jobs','Keep this lesson','active',1,1,'example.com'); PRAGMA user_version=3;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("resume partial migration: %v", err)
	}
	defer s.Close()
	lessons, err := s.Lessons()
	if err != nil || len(lessons) != 1 || lessons[0].Scope != "example.com" || lessons[0].Lesson != "Keep this lesson" {
		t.Fatalf("lesson not preserved: %+v %v", lessons, err)
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
}
