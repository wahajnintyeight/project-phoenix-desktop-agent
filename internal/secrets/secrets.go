// Package secrets stores API keys encrypted with Windows DPAPI and resolves the references
// config.yaml uses to point at them.
//
// DPAPI ties a blob to the Windows user (and machine). That protects the file if it is
// copied away, backed up, or read by another account. It does NOT protect against other
// programs running as the same user, which can ask DPAPI to decrypt too. State that plainly
// to users; a master password is the planned upgrade.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ErrNotFound means no secret is stored under that name.
var ErrNotFound = errors.New("secret not found")

// Store is a directory of DPAPI-encrypted files, one per secret.
type Store struct{ dir string }

// Open returns a store rooted at dir, creating it (owner-only) if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// DefaultDir is %LOCALAPPDATA%\Phoenix\vault.
func DefaultDir() (string, error) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(local, "Phoenix", "vault"), nil
}

func (s *Store) path(name string) (string, error) {
	if !nameRE.MatchString(name) {
		return "", fmt.Errorf("invalid secret name %q", name)
	}
	return filepath.Join(s.dir, name+".dpapi"), nil
}

// Set encrypts and stores value under name, replacing any previous value.
func (s *Store) Set(name, value string) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if value == "" {
		return errors.New("empty secret")
	}
	blob, err := protect([]byte(value))
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Get decrypts and returns the secret.
func (s *Store) Get(name string) (string, error) {
	p, err := s.path(name)
	if err != nil {
		return "", err
	}
	blob, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	plain, err := unprotect(blob)
	if err != nil {
		return "", fmt.Errorf("decrypt %s: %w (the vault is tied to this Windows user and machine)", name, err)
	}
	return string(plain), nil
}

// Has reports whether a secret exists, without decrypting it.
func (s *Store) Has(name string) bool {
	p, err := s.path(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Delete removes a secret. Deleting a missing one is not an error.
func (s *Store) Delete(name string) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RefKind says where a key reference points.
type RefKind string

const (
	RefNone    RefKind = ""
	RefVault   RefKind = "vault"
	RefEnv     RefKind = "env"
	RefLiteral RefKind = "literal"
)

// Resolve turns a config reference into its value: "vault:name", "env:NAME", or a literal
// string. An empty reference resolves to an empty value with no error.
func (s *Store) Resolve(ref string) (value string, kind RefKind, err error) {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "":
		return "", RefNone, nil
	case strings.HasPrefix(ref, "vault:"):
		v, err := s.Get(strings.TrimPrefix(ref, "vault:"))
		if errors.Is(err, ErrNotFound) {
			return "", RefVault, nil // referenced but not stored yet: reads as "no key"
		}
		return v, RefVault, err
	case strings.HasPrefix(ref, "env:"):
		return os.Getenv(strings.TrimPrefix(ref, "env:")), RefEnv, nil
	}
	return ref, RefLiteral, nil
}
