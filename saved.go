package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

type SavedPodcast struct {
	Title      string `json:"title"`
	FeedURL    string `json:"feedURL"`
	ArtworkURL string `json:"artworkURL"`
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

func saveSaved(s SavedPodcasts) {
	path := savedPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(path, data, 0644)
}

// savedSortedURLs returns feed URLs sorted alphabetically by podcast title.
func savedSortedURLs(s SavedPodcasts) []string {
	urls := make([]string, 0, len(s))
	for url := range s {
		urls = append(urls, url)
	}
	sort.Slice(urls, func(i, j int) bool {
		return s[urls[i]].Title < s[urls[j]].Title
	})
	return urls
}
