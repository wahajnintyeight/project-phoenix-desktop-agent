package library

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReviewedReplacementAndScopedRetrieval(t *testing.T) {
	s := memStore(t)
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	old, err := s.ProposeLesson("preference", "job applications", "Use old resume", "Old preference", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewLesson(old.ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	newLesson, err := s.ProposeLesson("preference", "job applications", "Use new resume", "User corrected preference", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewLessonDetails(newLesson.ID, "scope", "grafana", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewLessonDetails(newLesson.ID, "replace", "", old.ID); err != nil {
		t.Fatal(err)
	}
	got := s.RelevantLessons("grafana job applications", 1200)
	if !strings.Contains(got, "Use new resume") || !strings.Contains(got, "scope: grafana") || strings.Contains(got, "Use old resume") {
		t.Fatalf("replacement: %q", got)
	}
	if got := s.RelevantLessons("other job applications", 1200); got != "" {
		t.Fatalf("scope leaked: %q", got)
	}
	if got := s.RelevantLessonsWithScopes("job applications", []string{"grafana"}, 1200); !strings.Contains(got, "Use new resume") {
		t.Fatalf("trusted scope missing: %q", got)
	}
	if got := s.RelevantLessonsWithScopes("job applications", []string{"other"}, 1200); got != "" {
		t.Fatalf("unrelated trusted scope leaked: %q", got)
	}
	if err := s.ReviewLessonDetails(newLesson.ID, "replace", "", old.ID); err == nil {
		t.Fatal("replaced a non-active lesson")
	}
}

func TestLessonRetrievalUsesWholeWordsAndByteBudget(t *testing.T) {
	s := memStore(t)
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Java lessons one", "Java lessons two", "Java lessons three", "Java lessons four"} {
		l, err := s.ProposeLesson("workflow", "java", value, "fixture", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReviewLesson(l.ID, "approve", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.RelevantLessons("javascript", 1200); got != "" {
		t.Fatalf("substring match: %q", got)
	}
	got := s.RelevantLessons("java", 1200)
	if n := strings.Count(got, "- ["); n != 3 {
		t.Fatalf("got %d results: %q", n, got)
	}
	if got := s.RelevantLessons("java", 150); len(got) > 150 {
		t.Fatalf("byte budget exceeded: %d", len(got))
	}
}

func TestLessonMatchesWordsBeyondFirst24(t *testing.T) {
	s := memStore(t)
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	lesson, err := s.ProposeLesson("workflow", "workflow", "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mango november oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu sponsorship", "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewLesson(lesson.ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.RelevantLessons("sponsorship", 1200); !strings.Contains(got, "sponsorship") {
		t.Fatalf("missed later word: %q", got)
	}
}

func TestLearningBudgetValidation(t *testing.T) {
	s := memStore(t)
	if s.LearningContextBudget() != DefaultLearningContextBytes {
		t.Fatal("wrong default")
	}
	if err := s.SetLearningContextBudget(800); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLearningContextBudget(1); err == nil {
		t.Fatal("accepted invalid budget")
	}
	if s.LearningContextBudget() != 800 {
		t.Fatal("invalid budget changed the saved value")
	}
}

func TestPinnedLessonsAreExplicitAndBounded(t *testing.T) {
	s := memStore(t)
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, name := range []string{"first", "second", "third"} {
		l, err := s.ProposeLesson("preference", name, "Always ask", "fixture", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReviewLesson(l.ID, "pin", "", ""); err == nil {
			t.Fatal("pinned unapproved lesson")
		}
		if err := s.ReviewLesson(l.ID, "approve", "", ""); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, l.ID)
	}
	for _, id := range ids[:2] {
		if err := s.ReviewLesson(id, "pin", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReviewLesson(ids[2], "pin", "", ""); err == nil {
		t.Fatal("pinned a third lesson")
	}
	pinned := s.PinnedLessons()
	if len(pinned) > 398 || !strings.Contains(pinned, "first") || !strings.Contains(pinned, "second") {
		t.Fatalf("unexpected pins: %q", pinned)
	}
	if got := s.RelevantLessons("first", 800); strings.Contains(got, "Always ask") {
		t.Fatalf("pinned lesson duplicated: %q", got)
	}
	if err := s.ReviewLessonDetails(ids[0], "scope", "grafana", 0); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.PinnedLessons(), "first") {
		t.Fatal("scoped lesson remained globally pinned")
	}
	if err := s.ReviewLesson(ids[1], "unhelpful", "", ""); err != nil {
		t.Fatal(err)
	}
	if s.PinnedLessons() != "" {
		t.Fatal("unhelpful lesson remained pinned")
	}
}

func TestConcurrentLearningCapacityAcrossProcesses(t *testing.T) {
	if path := os.Getenv("PHOENIX_TEST_LEARNING_DB"); path != "" {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(path + ".go"); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("start signal timed out")
			}
			time.Sleep(10 * time.Millisecond)
		}
		_, err = s.ProposeLesson("workflow", "concurrent "+os.Getenv("PHOENIX_TEST_LEARNING_NAME"), "new lesson", "fixture", "")
		if err != nil && !strings.Contains(err.Error(), "full") {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "library.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLessons-1; i++ {
		if _, err := s.db.Exec(`INSERT INTO learned_items(kind,trigger_text,lesson,created_at,updated_at) VALUES('workflow',?,'fixture',1,1)`, "fixture "+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	var cmds []*exec.Cmd
	var outputs []*bytes.Buffer
	for _, name := range []string{"a", "b"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentLearningCapacityAcrossProcesses$")
		cmd.Env = append(os.Environ(), "PHOENIX_TEST_LEARNING_DB="+path, "PHOENIX_TEST_LEARNING_NAME="+name)
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
		outputs = append(outputs, &output)
	}
	if err := os.WriteFile(path+".go", []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("child failed: %v: %s", err, outputs[i].String())
		}
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM learned_items`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != maxLessons {
		t.Fatalf("unexpected count after concurrent inserts: %d", count)
	}
}

func TestConcurrentReplacementsLeaveOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	old, err := first.ProposeLesson("workflow", "resume", "Use old file", "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ReviewLesson(old.ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	a, err := first.ProposeLesson("workflow", "resume", "Use file A", "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.ProposeLesson("workflow", "resume", "Use file B", "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; results <- first.ReviewLessonDetails(a.ID, "replace", "", old.ID) }()
	go func() { <-start; results <- second.ReviewLessonDetails(b.ID, "replace", "", old.ID) }()
	close(start)
	wins := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("got %d successful replacements", wins)
	}
	lessons, err := first.Lessons()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, l := range lessons {
		counts[l.Status]++
	}
	if counts["active"] != 1 || counts["candidate"] != 1 || counts["superseded"] != 1 {
		t.Fatalf("non-atomic review: %+v", lessons)
	}
}

func TestLongQueryKeepsScopeAndBudget(t *testing.T) {
	s := memStore(t)
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	l, err := s.ProposeLesson("workflow", "job search", "Check sponsor policy", "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewLessonDetails(l.ID, "scope", "example.com", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewLesson(l.ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	query := "job search " + strings.Repeat("filler ", 2800) + "example.com"
	result := s.RelevantLessons(query, 400)
	if !strings.Contains(result, "Check sponsor policy") || len(result) > 400 {
		t.Fatalf("long query: %d bytes %q", len(result), result)
	}
	if got := s.RelevantLessons(strings.ReplaceAll(query, "example.com", "different.com"), 400); got != "" {
		t.Fatalf("scope leaked: %q", got)
	}
}
