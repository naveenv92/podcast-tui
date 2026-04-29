package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mmcdole/gofeed"
)

type HistoryEntry struct {
	Title        string        `json:"title"`
	FeedURL      string        `json:"feedURL"`
	PodcastTitle string        `json:"podcastTitle"`
	Progress     time.Duration `json:"progress"`
	Completed    bool          `json:"completed"`
	ListenedAt   time.Time     `json:"listenedAt"`
}

type ListeningStats struct {
	TotalTime         time.Duration
	MostListenedTitle string
}

func (h History) computeStats() ListeningStats {
	var total time.Duration
	byFeed := make(map[string]time.Duration)
	nameFor := make(map[string]string)

	for _, entry := range h {
		total += entry.Progress
		if entry.FeedURL != "" {
			byFeed[entry.FeedURL] += entry.Progress
			if entry.PodcastTitle != "" {
				nameFor[entry.FeedURL] = entry.PodcastTitle
			}
		}
	}

	var topURL string
	var topTime time.Duration
	for feedURL, t := range byFeed {
		if t > topTime {
			topTime = t
			topURL = feedURL
		}
	}

	name := nameFor[topURL]
	if name == "" {
		name = topURL
	}

	return ListeningStats{TotalTime: total, MostListenedTitle: name}
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
