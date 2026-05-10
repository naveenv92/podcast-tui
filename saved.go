package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SavedPodcast struct {
	Title      string   `json:"title"`
	FeedURL    string   `json:"feedURL"`
	ArtworkURL string   `json:"artworkURL"`
	Categories []string `json:"categories,omitempty"`
}

type SavedPodcasts map[string]SavedPodcast

func savedPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "podcast-tui", "saved.json")
}

func loadSaved() SavedPodcasts {
	s := make(SavedPodcasts)
	data, err := os.ReadFile(savedPath())
	if err != nil {
		return s
	}
	json.Unmarshal(data, &s)
	return s
}

func clearSaved() SavedPodcasts {
	s := make(SavedPodcasts)
	saveSaved(s)
	return s
}

func saveSaved(s SavedPodcasts) {
	path := savedPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(path, data, 0644)
}

// sortKey strips a leading "the " (case-insensitive) for alphabetical sorting.
func sortKey(title string) string {
	if strings.HasPrefix(strings.ToLower(title), "the ") {
		return title[4:]
	}
	return title
}

// savedSortedURLs returns feed URLs sorted alphabetically by podcast title,
// ignoring a leading "The".
func savedSortedURLs(s SavedPodcasts) []string {
	urls := make([]string, 0, len(s))
	for url := range s {
		urls = append(urls, url)
	}
	sort.Slice(urls, func(i, j int) bool {
		return sortKey(s[urls[i]].Title) < sortKey(s[urls[j]].Title)
	})
	return urls
}

// savedSortedByDateURLs returns feed URLs sorted by most recent episode date
// descending, with A-Z as a tiebreaker for podcasts with the same or missing date.
func savedSortedByDateURLs(s SavedPodcasts, dates map[string]time.Time) []string {
	urls := make([]string, 0, len(s))
	for url := range s {
		urls = append(urls, url)
	}
	sort.Slice(urls, func(i, j int) bool {
		di, dj := dates[urls[i]], dates[urls[j]]
		if !di.Equal(dj) {
			return di.After(dj)
		}
		return sortKey(s[urls[i]].Title) < sortKey(s[urls[j]].Title)
	})
	return urls
}
