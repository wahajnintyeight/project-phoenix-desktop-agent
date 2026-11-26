package library

import (
	"strings"
	"testing"
)

// Thresholds set before the first run: at least 8/10 direct hits, no unrelated
// results on the five unrelated queries. Paraphrases are reported separately.
func TestLearningBroaderEvaluation(t *testing.T) {
	s := memStore(t)
	if err := s.SetLearningEnabled(true); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct{ trigger, lesson string }{
		{"garden watering", "WATER_MORNING"}, {"invoice currency", "INVOICE_EUR"},
		{"flight booking", "FLIGHT_DIRECT"}, {"meeting schedule", "MEET_AFTERNOON"},
		{"résumé format", "RESUME_A4"}, {"archive backup", "BACKUP_ENCRYPT"},
		{"job sponsorship", "CHECK_VISA"}, {"newsletter draft", "REVIEW_RECIPIENT"},
		{"restaurant reservation", "BOOK_OUTDOORS"}, {"software deployment", "DEPLOY_STAGING"},
	}
	for _, f := range fixtures {
		l, err := s.ProposeLesson("workflow", f.trigger, f.lesson, "Synthetic evaluation", "")
		if err != nil {
			t.Fatal(err)
		}
		if err = s.ReviewLesson(l.ID, "approve", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		query, want string
		group       string
	}{
		{"garden watering", "WATER_MORNING", "direct"}, {"invoice currency", "INVOICE_EUR", "direct"},
		{"flight booking", "FLIGHT_DIRECT", "direct"}, {"meeting schedule", "MEET_AFTERNOON", "direct"},
		{"résumé format", "RESUME_A4", "direct"}, {"archive backup", "BACKUP_ENCRYPT", "direct"},
		{"job sponsorship", "CHECK_VISA", "direct"}, {"newsletter draft", "REVIEW_RECIPIENT", "direct"},
		{"restaurant reservation", "BOOK_OUTDOORS", "direct"}, {"software deployment", "DEPLOY_STAGING", "direct"},
		{"water the plants", "WATER_MORNING", "paraphrase"}, {"arrange a time to talk", "MEET_AFTERNOON", "paraphrase"},
		{"choose a nonstop plane", "FLIGHT_DIRECT", "paraphrase"}, {"make a table booking", "BOOK_OUTDOORS", "paraphrase"},
		{"ship the application to production", "DEPLOY_STAGING", "paraphrase"},
		{"paint a landscape", "", "unrelated"}, {"cook lentil soup", "", "unrelated"},
		{"play a chess game", "", "unrelated"}, {"hello", "", "unrelated"}, {"choose a bicycle", "", "unrelated"},
	}
	for _, mode := range []string{"none", "legacy", "current"} {
		hits, paraphrases, falseCases, bytes := 0, 0, 0, 0
		for _, c := range cases {
			out := ""
			if mode == "legacy" {
				out = holdoutLegacy(t, s, c.query, 1200)
			}
			if mode == "current" {
				out = s.RelevantLessons(c.query, 1200)
			}
			bytes += len(out)
			if c.want != "" && strings.Contains(out, c.want) {
				if c.group == "direct" {
					hits++
				} else {
					paraphrases++
				}
			}
			if c.group == "unrelated" && out != "" {
				falseCases++
			}
			if len(out) > 1200 {
				t.Fatalf("%s exceeds budget", mode)
			}
		}
		t.Logf("%s: direct=%d/10 paraphrase=%d/5 unrelated=%d/5 bytes=%d", mode, hits, paraphrases, falseCases, bytes)
		if mode == "current" && (hits < 8 || falseCases > 0) {
			t.Errorf("fixed direct/unrelated gate failed")
		}
	}
}
