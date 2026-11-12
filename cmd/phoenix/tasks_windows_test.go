package main

import (
	"strings"
	"testing"

	"phoenix/internal/library"
)

func TestTaskOutcome(t *testing.T) {
	for _, c := range []struct{ in, status, report string }{
		{"DONE: 4 applied, 2 skipped", "ok", "4 applied, 2 skipped"},
		{"Working on it.\n**DONE:** sent 3 emails", "ok", "sent 3 emails"},
		{"BLOCKED: LinkedIn asks for a login", "blocked", "LinkedIn asks for a login"},
		{"DONE: all clear\n```choices\nNext?\n- Again\n```", "ok", "all clear"},
		{"DONE: all clear\n```actions\nNext?\n- Explore Acme\n```", "ok", "all clear"},
		{"DONE: all clear\n```suggestions\nSuggested next steps\n- Again\n```", "ok", "all clear"},
		{"", "ok", "Finished with no report."},
		{"No marker here", "ok", "No marker here"},
	} {
		s, r := taskOutcome(c.in)
		if s != c.status || r != c.report {
			t.Errorf("%q: got %q %q", c.in, s, r)
		}
	}
}

func TestTaskBriefCarriesLastResult(t *testing.T) {
	b := taskBrief(library.Task{Name: "Sweep", Prompt: "Do X", LastRun: 1700000000, LastStatus: "ok", LastResult: "applied to Acme"})
	for _, want := range []string{"Sweep", "Do X", "applied to Acme", "DONE:", "BLOCKED:", "Do not end with a choices block"} {
		if !strings.Contains(strings.ToLower(b), strings.ToLower(want)) {
			t.Errorf("brief lacks %q", want)
		}
	}
	if strings.Contains(taskBrief(library.Task{Name: "N", Prompt: "P"}), "Last run") {
		t.Error("first run has no last result")
	}
}

func TestGoalTaskBriefRequiresRequestedOutcome(t *testing.T) {
	b := strings.ToLower(taskBrief(library.Task{Name: "Find an eligible job", Prompt: "Find one eligible job.", Goal: true}))
	for _, want := range []string{"for search or hunt goals, finding no eligible result is not success", "if none remain, finish as blocked", "only when the requested outcome is verified"} {
		if !strings.Contains(b, want) {
			t.Errorf("goal brief lacks %q", want)
		}
	}
}
