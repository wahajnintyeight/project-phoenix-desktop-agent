package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripAndEncryptedOnDisk(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const secret = "sk-test-DO-NOT-USE-1234567890"
	if err := s.Set("claude", secret); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("claude")
	if err != nil || got != secret {
		t.Fatalf("Get = %q, %v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "claude.dpapi"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-test") {
		t.Fatal("secret is stored in plain text")
	}
	if !s.Has("claude") || s.Has("codex") {
		t.Fatal("Has is wrong")
	}
	if err := s.Delete("claude"); err != nil || s.Has("claude") {
		t.Fatalf("Delete failed: %v", err)
	}
	if err := s.Delete("claude"); err != nil {
		t.Fatalf("deleting a missing secret should not fail: %v", err)
	}
	if _, err := s.Get("claude"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestInvalidNamesAreRejected(t *testing.T) {
	s, _ := Open(t.TempDir())
	for _, n := range []string{"", "../x", "a/b", `a\b`, "UPPER", ".hidden", strings.Repeat("a", 80)} {
		if err := s.Set(n, "v"); err == nil {
			t.Errorf("name %q was accepted", n)
		}
	}
}

func TestResolve(t *testing.T) {
	s, _ := Open(t.TempDir())
	t.Setenv("PHX_TEST_KEY", "from-env")
	_ = s.Set("k", "from-vault")

	for _, c := range []struct {
		ref, want string
		kind      RefKind
	}{
		{"", "", RefNone},
		{"vault:k", "from-vault", RefVault},
		{"vault:missing", "", RefVault},
		{"env:PHX_TEST_KEY", "from-env", RefEnv},
		{"env:PHX_NOT_SET", "", RefEnv},
		{"sk-literal", "sk-literal", RefLiteral},
	} {
		v, kind, err := s.Resolve(c.ref)
		if err != nil || v != c.want || kind != c.kind {
			t.Errorf("Resolve(%q) = %q, %q, %v; want %q, %q", c.ref, v, kind, err, c.want, c.kind)
		}
	}
}
