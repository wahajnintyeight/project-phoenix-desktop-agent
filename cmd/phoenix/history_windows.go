package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"phoenix/internal/config"
)

const (
	historyLimit       = 100
	historyDisplaySize = 8
	historyURLMaxBytes = 4096
	historyFileMaxSize = 1 << 20
)

type historyEntry struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Host      string    `json:"host"`
	VisitedAt time.Time `json:"visitedAt"`
}

type historyItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Host  string `json:"host"`
}

var (
	historyPath   string
	recentHistory []historyEntry
)

func initHistory() {
	if profile := os.Getenv("PHOENIX_TEST_PROFILE"); profile != "" {
		historyPath = filepath.Join(profile, "history.json")
	} else if configPath, err := config.DefaultPath(); err == nil {
		historyPath = filepath.Join(filepath.Dir(configPath), "history.json")
	} else {
		logf("history path: %v", err)
		return
	}

	info, err := os.Stat(historyPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		logf("inspect history: %v", err)
		return
	}
	if info.Size() > historyFileMaxSize {
		logf("ignore oversized history file (%d bytes)", info.Size())
		return
	}
	data, err := os.ReadFile(historyPath)
	if err != nil {
		logf("read history: %v", err)
		return
	}
	var entries []historyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		logf("decode history: %v", err)
		return
	}
	for _, entry := range entries {
		urlValue, host, ok := cleanHistoryURL(entry.URL)
		if !ok {
			continue
		}
		entry.URL, entry.Host = urlValue, host
		entry.Title = cleanHistoryTitle(entry.Title, host)
		recentHistory = append(recentHistory, entry)
		if len(recentHistory) == historyLimit {
			break
		}
	}
}

func cleanHistoryURL(raw string) (string, string, bool) {
	if len(raw) == 0 || len(raw) > historyURLMaxBytes || strings.ContainsAny(raw, "\x00\r\n") {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Hostname() == "" {
		return "", "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", false
	}
	u.Host = strings.ToLower(u.Host)
	// Keep navigable query paths, but discard fragments such as OAuth tokens and anchors.
	u.Fragment, u.RawFragment = "", ""
	return u.String(), u.Host, true
}

func cleanHistoryTitle(title, fallback string) string {
	title = strings.TrimSpace(strings.ToValidUTF8(title, "�"))
	if title == "" {
		title = fallback
	}
	runes := []rune(title)
	if len(runes) > 160 {
		title = string(runes[:160])
	}
	return title
}

func recordVisited(rawURL, title string) {
	urlValue, host, ok := cleanHistoryURL(rawURL)
	if !ok {
		return
	}
	entry := historyEntry{
		URL:       urlValue,
		Title:     cleanHistoryTitle(title, host),
		Host:      host,
		VisitedAt: time.Now(),
	}
	for i := 0; i < len(recentHistory); i++ {
		if recentHistory[i].URL == urlValue {
			recentHistory = append(recentHistory[:i], recentHistory[i+1:]...)
			break
		}
	}
	recentHistory = append([]historyEntry{entry}, recentHistory...)
	if len(recentHistory) > historyLimit {
		recentHistory = recentHistory[:historyLimit]
	}
	persistHistory()
	pushHistory()
}

func updateVisitedTitle(rawURL, title string) {
	urlValue, host, ok := cleanHistoryURL(rawURL)
	if !ok {
		return
	}
	title = cleanHistoryTitle(title, host)
	for i := range recentHistory {
		if recentHistory[i].URL == urlValue && recentHistory[i].Title != title {
			recentHistory[i].Title = title
			persistHistory()
			pushHistory()
			return
		}
	}
}

func persistHistory() {
	if historyPath == "" {
		return
	}
	dir := filepath.Dir(historyPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		logf("create history folder: %v", err)
		return
	}
	data, err := json.Marshal(recentHistory)
	if err != nil {
		logf("encode history: %v", err)
		return
	}
	tmp := historyPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		logf("write history: %v", err)
		return
	}
	if err := os.Rename(tmp, historyPath); err != nil {
		_ = os.Remove(tmp)
		logf("save history: %v", err)
	}
}

func pushHistory() {
	if !ready {
		return
	}
	count := min(len(recentHistory), historyDisplaySize)
	items := make([]historyItem, count)
	for i, entry := range recentHistory[:count] {
		items[i] = historyItem{
			ID:    base64.RawURLEncoding.EncodeToString([]byte(entry.URL)),
			Title: entry.Title,
			Host:  entry.Host,
		}
	}
	post("u" + fs + "recent-sites" + fs + "history" + fs + jsonString(items))
}

func openHistoryEntry(id string) {
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return
	}
	for _, entry := range recentHistory {
		if string(decoded) == entry.URL {
			navigateActive(entry.URL)
			return
		}
	}
}

func clearHistory() {
	recentHistory = nil
	if historyPath != "" {
		if err := os.Remove(historyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			logf("clear history: %v", err)
		}
	}
	pushHistory()
}
