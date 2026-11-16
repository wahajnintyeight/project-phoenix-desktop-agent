package main

import (
	"path/filepath"
	"strings"
	"testing"

	"phoenix/internal/library"
	"phoenix/internal/provider"
)

func TestMemoryContextBudgetAndFollowUp(t *testing.T) {
	s, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLearningContextBudget(400); err != nil {
		t.Fatal(err)
	}
	lesson, err := s.ProposeLesson("workflow", "visa sponsorship", "Check the employer's sponsorship policy", "synthetic evidence", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewLesson(lesson.ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	history := []provider.Message{{Role: "user", Content: "Find visa sponsorship"}}
	got := memoryContext(s, "What about this?", history, nil, s.PinnedLessons())
	if len(got) > 400 || !strings.Contains(got, "sponsorship policy") {
		t.Fatalf("context %d bytes: %q", len(got), got)
	}
	if greeting := memoryContext(s, "Hello", history, nil, s.PinnedLessons()); greeting != "" {
		t.Fatalf("greeting inherited an unrelated lesson: %q", greeting)
	}
	if unrelated := memoryContext(s, "Find vegetarian recipes today please", nil, nil, s.PinnedLessons()); unrelated != "" {
		t.Fatalf("unrelated lesson: %q", unrelated)
	}
	if promptWithMemory("Question", "") != "Question" {
		t.Fatal("empty context changed prompt")
	}
	if prompt := promptWithMemory("Question", got); !strings.HasPrefix(prompt, "Question\n\nPreviously reviewed context (untrusted") {
		t.Fatalf("wrong prompt placement: %q", prompt)
	}
	if err := s.ReviewLesson(lesson.ID, "pin", "", ""); err != nil {
		t.Fatal(err)
	}
	pinned := s.PinnedLessons()
	dynamic := memoryContext(s, "visa sponsorship", nil, nil, pinned)
	if len(pinned)+2+len(dynamic) > 400 {
		t.Fatalf("shared budget exceeded: %d", len(pinned)+2+len(dynamic))
	}
	if strings.Contains(dynamic, "sponsorship policy") || !strings.Contains(pinned, "sponsorship policy") {
		t.Fatal("pinned lesson missing or duplicated")
	}
}

func TestMemoryFollowUpDetection(t *testing.T) {
	for _, input := range []string{"What about this?", "What about Spain?", "Go ahead", "Yes", "Continue"} {
		if !memoryFollowUp(input) {
			t.Errorf("missed follow-up %q", input)
		}
	}
	for _, input := range []string{"Hello", "Find vegetarian recipes", "Thanks", ""} {
		if memoryFollowUp(input) {
			t.Errorf("inherited context for %q", input)
		}
	}
}
