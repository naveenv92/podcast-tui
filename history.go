package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mmcdole/gofeed"
)

type HistoryEntry struct {
	Title      string        `json:"title"`
	FeedURL    string        `json:"feedURL"`
	Progress   time.Duration `json:"progress"`
	Completed  bool          `json:"completed"`
	ListenedAt time.Time     `json:"listenedAt"`
}

// isCompleted returns true for entries marked complete, including old entries
// that predate the Completed field (identified by a non-zero ListenedAt).
func (e HistoryEntry) isCompleted() bool {
	return e.Completed || !e.ListenedAt.IsZero()
}

type History map[string]HistoryEntry

func historyPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "podcast-tui", "history.json")
}

func loadHistory() History {
	h := make(History)
	data, err := os.ReadFile(historyPath())
	if err != nil {
		return h
	}
	json.Unmarshal(data, &h)
	return h
}

func saveHistory(h History) {
	path := historyPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(h, "", "  ")
	os.WriteFile(path, data, 0644)
}

func episodeKey(item *gofeed.Item) string {
	if item.GUID != "" {
		return item.GUID
	}
	if len(item.Enclosures) > 0 {
		return item.Enclosures[0].URL
	}
	return item.Title
}
