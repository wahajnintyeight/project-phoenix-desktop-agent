package library

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLearningLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.LearningEnabled() {
		t.Fatal("learning should default off")
	}
	if _, err := s.ProposeLesson("workflow", "job application", "Read the listing first", "Listing was read", "chat-abc"); err == nil {
		t.Fatal("disabled learning accepted a proposal")
	}
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	l, err := s.ProposeLesson("workflow", "job application", "Read the listing first", "Listing was read", "chat-abc")
	if err != nil || l.Status != "candidate" || l.SourceChat != "chat-abc" {
		t.Fatalf("proposal: %+v %v", l, err)
	}
	if got := s.RelevantLessons("job application", 1000); got != "" {
		t.Fatalf("unapproved lesson was used: %s", got)
	}
	again, err := s.ProposeLesson("workflow", "Job application", "Read the listing first", "another claim", "chat-def")
	if err != nil || again.ID != l.ID || again.Evidence != "Listing was read" {
		t.Fatalf("duplicate changed evidence: %+v %v", again, err)
	}
	if err := s.ReviewLesson(l.ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.RelevantLessons("apply to this job", 1000); !strings.Contains(got, "Read the listing first") {
		t.Fatalf("missing approved lesson: %q", got)
	}
	if got := s.RelevantLessons("translate this recipe", 1000); got != "" {
		t.Fatalf("unrelated lesson: %q", got)
	}
	if err := s.ReviewLesson(l.ID, "edit", "job application", "Inspect the latest listing"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewLesson(l.ID, "helpful", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.Lessons()
	if err != nil || !s.LearningEnabled() || len(list) != 1 || list[0].Helpful != 1 || list[0].Lesson != "Inspect the latest listing" {
		t.Fatalf("reload: %+v %v", list, err)
	}
	if err := s.ReviewLesson(l.ID, "unhelpful", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.RelevantLessons("job application", 1000); got != "" {
		t.Fatalf("lesson needing review was still used: %q", got)
	}
	if err := s.ReviewLesson(l.ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLearningEnabled(false); err != nil {
		t.Fatal(err)
	}
	if got := s.RelevantLessons("job application", 1000); !strings.Contains(got, "Inspect the latest listing") {
		t.Fatal("pausing learning should preserve approved lessons")
	}
	if err := s.ReviewLesson(l.ID, "delete", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProposeLesson("workflow", "job application", "Inspect the latest listing", "Listing was read", ""); err == nil {
		t.Fatal("identical forgotten lesson was suggested again")
	}
	if got := s.RelevantLessons("job application", 1000); got != "" {
		t.Fatalf("forgotten lesson returned: %q", got)
	}
}

func TestLearningToolRequiresReviewAndHasSource(t *testing.T) {
	s := memStore(t)
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	a, b := sdk.NewInMemoryTransports()
	server, err := NewServerForSession(s, "agent", "chat-123").Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(context.Background(), &sdk.CallToolParams{Name: "learning_propose", Arguments: map[string]any{"kind": "experience", "trigger": "PDF application", "lesson": "Check PDF type", "evidence": "The page required a PDF"}})
	if err != nil || result.IsError {
		t.Fatalf("tool: %+v %v", result, err)
	}
	lessons, err := s.Lessons()
	if err != nil || len(lessons) != 1 || lessons[0].SourceChat != "chat-123" || lessons[0].Status != "candidate" {
		t.Fatalf("lessons: %+v %v", lessons, err)
	}
	if err := s.ClearMemories(); err != nil {
		t.Fatal(err)
	}
	lessons, err = s.Lessons()
	if err != nil || len(lessons) != 0 {
		t.Fatalf("clear left lessons: %+v %v", lessons, err)
	}
}

func TestRelevantMemoriesAndUnlabelledFacts(t *testing.T) {
	s := memStore(t)
	if _, err := s.Remember("preference", "Work", "", "Remote jobs"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember("preference", "Work", "", "Flexible hours"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember("preference", "Food", "Diet", "Vegetarian"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember("preference", "General", "Response style", "Be concise"); err != nil {
		t.Fatal(err)
	}
	all, err := s.Memories()
	if err != nil || len(all) != 4 {
		t.Fatalf("unlabelled memories overwritten: %+v %v", all, err)
	}
	got := s.RelevantMemoryDigest("find remote jobs", 500)
	if !strings.Contains(got, "Remote jobs") || strings.Contains(got, "Vegetarian") {
		t.Fatalf("wrong memories: %q", got)
	}
	if got := s.RelevantMemoryDigest("hi", 500); !strings.Contains(got, "Be concise") || strings.Contains(got, "Vegetarian") {
		t.Fatalf("global style or unrelated memory incorrect: %q", got)
	}
}
