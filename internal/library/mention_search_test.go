package library

import "testing"

func TestMentionSearchNamesAndPaths(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Write("career/cv 100%.txt", "private needle", Create, "user"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Write("career/other.txt", "private needle", Create, "user"); err != nil {
		t.Fatal(err)
	}
	got, err := s.MentionSearch("100%", 50)
	if err != nil || len(got) != 1 || got[0].Path != "career/cv 100%.txt" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, err = s.MentionSearch("career", 50)
	if err != nil || len(got) != 1 || !got[0].Dir || got[0].Path != "career" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, err = s.MentionSearch("private needle", 50)
	if err != nil || len(got) != 0 {
		t.Fatalf("content should not match: %+v, %v", got, err)
	}
	got, err = s.MentionSearch("", 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("limit: %+v, %v", got, err)
	}
}
