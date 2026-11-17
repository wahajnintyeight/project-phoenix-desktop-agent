package voice

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// shortcut: this ledger tracks Phoenix only; use Cartesia account usage before sharing the key across apps.
	CreditLimit = 20000
	window      = 28 * 24 * time.Hour
)

var ErrBudgetExceeded = errors.New("Cartesia free-use limit reached")

type reservation struct {
	At      time.Time `json:"at"`
	Credits int       `json:"credits"`
}

type budgetFile struct {
	Reservations []reservation `json:"reservations"`
}

type Budget struct {
	mu   sync.Mutex
	path string
}

func NewBudget(dir string) *Budget {
	return &Budget{path: filepath.Join(dir, "voice-credits.json")}
}

func (b *Budget) Usage() (used, remaining int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries, err := b.read()
	if err != nil {
		return 0, 0, err
	}
	used = sumRecent(entries, time.Now())
	return used, max(0, CreditLimit-used), nil
}

// Reserve commits usage before the request; failed calls stay counted to prevent a retry from
// pushing the free allowance past its limit.
func (b *Budget) Reserve(credits int) (used, remaining int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	entries, err := b.read()
	if err != nil {
		return 0, 0, err
	}
	used = sumRecent(entries, now)
	if credits <= 0 || credits > CreditLimit-used {
		return used, max(0, CreditLimit-used), ErrBudgetExceeded
	}
	entries = recentReservations(entries, now)
	entries = append(entries, reservation{At: now, Credits: credits})
	data, err := json.Marshal(budgetFile{Reservations: entries})
	if err != nil {
		return used, max(0, CreditLimit-used), err
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return used, max(0, CreditLimit-used), err
	}
	if err := os.WriteFile(b.path, data, 0o600); err != nil {
		return used, max(0, CreditLimit-used), err
	}
	used += credits
	return used, max(0, CreditLimit-used), nil
}

func (b *Budget) read() ([]reservation, error) {
	data, err := os.ReadFile(b.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file budgetFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	return file.Reservations, nil
}

func sumRecent(entries []reservation, now time.Time) int {
	used := 0
	for _, entry := range entries {
		if entry.Credits > 0 && !entry.At.IsZero() && now.Sub(entry.At) < window {
			used += entry.Credits
		}
	}
	return used
}

func recentReservations(entries []reservation, now time.Time) []reservation {
	recent := entries[:0]
	for _, entry := range entries {
		if entry.Credits > 0 && !entry.At.IsZero() && now.Sub(entry.At) < window {
			recent = append(recent, entry)
		}
	}
	return recent
}
