package library

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// This fixture makes retrieval quality and the cost of unrelated matches visible.
func BenchmarkLearningBaseline(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLearningEnabled(true); err != nil {
		b.Fatal(err)
	}
	lessons := []struct{ trigger, lesson string }{
		{"job application", "Ask before submitting a job application"},
		{"visa sponsorship", "Check whether the employer sponsors visas"},
		{"resume PDF", "Use the latest resume PDF"},
		{"file export", "Export files to the desktop"},
	}
	for _, item := range lessons {
		l, err := s.ProposeLesson("workflow", item.trigger, item.lesson, "Synthetic evaluation", "")
		if err != nil {
			b.Fatal(err)
		}
		if err := s.ReviewLesson(l.ID, "approve", "", ""); err != nil {
			b.Fatal(err)
		}
	}
	cases := []struct{ query, want, reject string }{
		{"prepare my job application", "Ask before submitting", "Export files"},
		{"does this employer offer visa sponsorship", "sponsors visas", "resume PDF"},
		{"export file to desktop", "Export files", "job application"},
		{"translate a vegetarian recipe", "", "job application"},
	}
	var hits, falseHits, bytes int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := cases[i%len(cases)]
		got := s.RelevantLessons(c.query, 1400)
		if c.want != "" && strings.Contains(got, c.want) {
			hits++
		}
		if (c.want == "" && got != "") || strings.Contains(got, c.reject) {
			falseHits++
		}
		bytes += len(got)
	}
	b.ReportMetric(float64(hits)/float64(b.N), "hits/op")
	b.ReportMetric(float64(falseHits)/float64(b.N), "false-hits/op")
	b.ReportMetric(float64(bytes)/float64(b.N), "context-bytes/op")
}

// The maximum-size scan reveals local cost without imposing machine-specific timing gates.
func BenchmarkLearningAtCapacity(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLearningEnabled(true); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		lesson, err := s.ProposeLesson("workflow", fmt.Sprintf("topic%d application", i), fmt.Sprintf("Review document %d", i), "Synthetic benchmark", "")
		if err != nil {
			b.Fatal(err)
		}
		if err := s.ReviewLesson(lesson.ID, "approve", "", ""); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.RelevantLessons("topic127 application", 1200)
	}
}
