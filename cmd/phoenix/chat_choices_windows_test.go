package main

import (
	"strings"
	"testing"
)

func TestParseChoiceAnswers(t *testing.T) {
	answers := parseChoiceAnswers(`[{"question":" Board? ","answer":" Indeed "},{"question":"CV?","answer":"Upload"}]`)
	if len(answers) != 2 || answers[0].Question != "Board?" || answers[0].Answer != "Indeed" {
		t.Fatalf("unexpected answers: %+v", answers)
	}
	for _, invalid := range []string{
		`[]`, `not json`, `[{"question":"","answer":"a"}]`,
		`[{"question":"q","answer":""}]`,
		`[{"question":"q","answer":"` + strings.Repeat("a", 501) + `"}]`,
		`[{"question":"q","answer":"a"},{"question":"q","answer":"a"},{"question":"q","answer":"a"},{"question":"q","answer":"a"},{"question":"q","answer":"a"}]`,
		strings.Repeat("x", 16385),
	} {
		if got := parseChoiceAnswers(invalid); got != nil {
			t.Fatalf("accepted invalid answers: %q", invalid)
		}
	}
}
