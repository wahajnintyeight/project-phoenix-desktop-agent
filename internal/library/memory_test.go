package library

import (
	"path/filepath"
	"strings"
	"testing"
)

func memStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestRememberUpdatesAndCountsUses(t *testing.T) {
	st := memStore(t)
	a, _ := st.Remember("fact", "Contact", "Phone", "+1 555 0100")
	b, _ := st.Remember("fact", "contact", "phone", "+1 555 0100")
	if a.ID != b.ID || b.Uses != 2 {
		t.Fatalf("same entry expected, got %+v %+v", a, b)
	}
	c, _ := st.Remember("fact", "Contact", "Phone", "+1 555 0199")
	if c.ID != a.ID || c.Value != "+1 555 0199" || c.Uses != 1 {
		t.Fatalf("a changed value replaces and restarts the count: %+v", c)
	}
	if _, err := st.Remember("nonsense", "x", "y", "z"); err == nil {
		t.Error("bad kind accepted")
	}
	if _, err := st.Remember("fact", "x", "y", "  "); err == nil {
		t.Error("empty value accepted")
	}
}

func TestChoicesBecomeHabits(t *testing.T) {
	st := memStore(t)
	for i := 0; i < 3; i++ {
		if err := st.RecordChoice("Which next?", "Skip, next listing"); err != nil {
			t.Fatal(err)
		}
	}
	d := st.MemoryDigest(2000)
	if !strings.Contains(d, `"Which next?" they usually choose "Skip, next listing" (3 times)`) {
		t.Fatalf("digest = %q", d)
	}
}

func TestDigestRespectsBudgetAndPutsFactsFirst(t *testing.T) {
	st := memStore(t)
	_ = st.RecordChoice("Pick one", "A")
	_, _ = st.Remember("fact", "Contact", "Phone", "+1 555 0100")
	for i := 0; i < 30; i++ {
		_, _ = st.Remember("answer", "Job applications", strings.Repeat("q", 20)+string(rune('a'+i%26))+string(rune('a'+i/26)), strings.Repeat("v", 80))
	}
	d := st.MemoryDigest(700)
	if len([]rune(d)) > 900 || !strings.Contains(d, "Phone: +1 555 0100") || !strings.Contains(d, "more not shown") {
		t.Fatalf("len %d: %s", len([]rune(d)), d)
	}
	if st.MemoryDigest(2000) == "" {
		t.Error("empty digest")
	}
	if memStore(t).MemoryDigest(2000) != "" {
		t.Error("nothing remembered must add nothing to the prompt")
	}
}

func TestUpdateForgetClear(t *testing.T) {
	st := memStore(t)
	m, _ := st.Remember("fact", "Contact", "Email", "a@b.c")
	if err := st.UpdateMemory(m.ID, "Contact", "Email", "d@e.f"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.SearchMemories("d@e.f", 5)
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	if st.UpdateMemory(999, "x", "y", "z") == nil {
		t.Error("updating a missing entry succeeded")
	}
	_ = st.ForgetMemory(m.ID)
	_, _ = st.Remember("fact", "A", "b", "c")
	_ = st.ClearMemories()
	if all, _ := st.Memories(); len(all) != 0 {
		t.Fatalf("%+v", all)
	}
}
