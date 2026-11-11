// Package agentpolicy supplies task-specific guidance for Phoenix's browser agent.
package agentpolicy

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:embed browser.md
var browser string

//go:embed job.md
var job string

var jobTerms = regexp.MustCompile(`(?i)\b(job|jobs|career|careers|vacancy|vacancies|recruiter|recruiting|resume|résumé|cv|cover letter|job application|job posting|work authorization|work authorisation)\b`)

// Instructions selects guidance from user-authored text only. A short follow-up can inherit
// a recent job context; the untrusted content of a page never selects a skill by itself.
func Instructions(message string, previousUserMessages []string) string {
	useJob := jobTerms.MatchString(message)
	if !useJob && len(strings.Fields(message)) <= 5 {
		for i, seen := len(previousUserMessages)-1, 0; i >= 0 && seen < 3; i, seen = i-1, seen+1 {
			if jobTerms.MatchString(previousUserMessages[i]) {
				useJob = true
				break
			}
		}
	}
	if useJob {
		return browser + "\n\n" + job
	}
	return browser
}
