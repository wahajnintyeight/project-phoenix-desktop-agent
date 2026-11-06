package main

import (
	"fmt"
	"strings"
	"testing"

	"phoenix/internal/browser"
)

func TestFormatExtractPagesAndFilters(t *testing.T) {
	var lines []string
	for i := 0; i < 900; i++ {
		lines = append(lines, fmt.Sprintf("row %d filler text", i))
	}
	lines[800] = "Salary: 120k USD"
	res := extractResult{Title: "Jobs", Lines: lines, Links: [][2]string{{"https://x.test/a", "Apply now"}, {"https://x.test/b", "About"}}}
	state := browserTabState{ID: 3, Host: "x.test"}

	first := formatExtract(state, res, browser.ExtractArgs{}).Text
	if !strings.Contains(first, "offset=") || strings.Contains(first, "Salary") {
		t.Fatalf("first page should be a window with a continuation: %.200s", first)
	}
	got := formatExtract(state, res, browser.ExtractArgs{Query: "salary"}).Text
	if !strings.Contains(got, "Salary: 120k USD") || !strings.Contains(got, "row 799") || !strings.Contains(got, "row 801") || strings.Contains(got, "row 10 ") {
		t.Fatalf("query should keep the match and one line of context: %s", got)
	}
	if strings.Contains(got, "offset=") {
		t.Fatal("a short filtered result needs no continuation")
	}
	withLinks := formatExtract(state, res, browser.ExtractArgs{Query: "apply", Links: true}).Text
	if !strings.Contains(withLinks, "Apply now → https://x.test/a") || strings.Contains(withLinks, "About") {
		t.Fatalf("links should be filtered by query: %s", withLinks)
	}
}

func TestMatchWithContextMarksGaps(t *testing.T) {
	got := matchWithContext([]string{"a", "b", "hit", "c", "d", "e", "f", "hit2", "g"}, "hit", 1)
	want := []string{"b", "hit", "c", "…", "f", "hit2", "g"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v want %v", got, want)
	}
}
