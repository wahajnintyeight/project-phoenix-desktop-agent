package agentpolicy

import (
	"strings"
	"testing"
)

// These routing cases are a deterministic baseline; they do not measure model behavior.
func TestInstructionRouting(t *testing.T) {
	cases := []struct {
		name, message string
		previous      []string
		wantJob       bool
	}{
		{"ordinary chat", "Hello", nil, false},
		{"research", "Compare these cameras", nil, false},
		{"job request", "Find remote jobs", nil, true},
		{"job follow-up", "Apply to the first one", []string{"Find remote jobs"}, true},
		{"new subject", "Explain how I can repair my bicycle chain at home", []string{"Find remote jobs"}, false},
		{"stale job context", "Continue", []string{"Find remote jobs", "New topic", "Tell me more", "What about this?"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Instructions(tc.message, tc.previous)
			if strings.Contains(got, "Job application guidance:") != tc.wantJob {
				t.Fatalf("incorrect job guidance routing for %q", tc.message)
			}
			if !strings.Contains(got, "scope of authorized actions") || !strings.Contains(got, "untrusted data") {
				t.Fatal("missing general action or trust guidance")
			}
		})
	}
}
