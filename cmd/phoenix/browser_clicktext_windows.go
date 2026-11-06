package main

import (
	"strconv"
	"strings"
)

// bestClickTarget picks the control a label most likely means from a snapshot's refs: an exact
// label beats one that starts with it, which beats one that merely contains it; buttons and links
// beat other roles; and on a tie the control nearer the top of the page (lower ref number) wins.
func bestClickTarget(refs map[string]browserRef, text string) (string, bool) {
	want := strings.ToLower(strings.Join(strings.Fields(text), " "))
	if want == "" {
		return "", false
	}
	best, bestScore, bestIdx := "", 1<<30, 1<<30
	for ref, item := range refs {
		name := strings.ToLower(strings.Join(strings.Fields(item.Name), " "))
		score := 0
		switch {
		case name == want:
		case strings.HasPrefix(name, want):
			score = 10
		case strings.Contains(name, want):
			score = 20
		default:
			continue
		}
		if item.Role != "button" && item.Role != "link" {
			score += 5
		}
		idx := refIndexOf(ref)
		if score < bestScore || (score == bestScore && idx < bestIdx) {
			best, bestScore, bestIdx = ref, score, idx
		}
	}
	return best, best != ""
}

// refIndexOf reads the number after ":e" in a ref such as "@s8f3a1:e12".
func refIndexOf(ref string) int {
	i := strings.LastIndex(ref, ":e")
	if i < 0 {
		return 1 << 30
	}
	n, err := strconv.Atoi(ref[i+2:])
	if err != nil {
		return 1 << 30
	}
	return n
}

// firstLines returns at most n lines of s, joined with spaces, for a short error message.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " ")
}
