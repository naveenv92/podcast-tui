package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type importDoneMsg struct {
	history      History
	saved        SavedPodcasts
	historyCount int
	savedCount   int
	err          error
}

func findLatestExport(dir, pattern string) string {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(matches) == 0 {
		return ""
	}
	var latest string
	var latestTime time.Time
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = path
		}
	}
	return latest
}

func importHistoryCSV(path string) (History, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	h := make(History)
	for _, record := range records[1:] { // skip header row
		if len(record) < 6 {
			continue
		}
		title := record[0]
		podcastTitle := record[1]
		status := record[2]
		progressSecs, _ := strconv.Atoi(record[3])
		feedURL := record[5]

		var listenedAt time.Time
		if record[4] != "" {
			listenedAt, _ = time.ParseInLocation("2006-01-02 15:04:05", record[4], time.Local)
		}

		key := feedURL + "|" + title
		if len(record) >= 7 && record[6] != "" {
			key = record[6]
		}
		h[key] = HistoryEntry{
			Title:        title,
			FeedURL:      feedURL,
			PodcastTitle: podcastTitle,
			Progress:     time.Duration(progressSecs) * time.Second,
			Completed:    status == "Completed",
			ListenedAt:   listenedAt,
		}
	}
	return h, nil
}

func importSavedCSV(path string) (SavedPodcasts, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	s := make(SavedPodcasts)
	for _, record := range records[1:] { // skip header row
		if len(record) < 4 {
			continue
		}
		feedURL := record[1]
		var categories []string
		if record[3] != "" {
			categories = strings.Split(record[3], "|")
		}
		s[feedURL] = SavedPodcast{
			Title:      record[0],
			FeedURL:    feedURL,
			ArtworkURL: record[2],
			Categories: categories,
		}
	}
	return s, nil
}

func importData(historyPath, savedPath string, existing History, existingSaved SavedPodcasts) tea.Cmd {
	return func() tea.Msg {
		merged := make(History, len(existing))
		for k, v := range existing {
			merged[k] = v
		}
		mergedSaved := make(SavedPodcasts, len(existingSaved))
		for k, v := range existingSaved {
			mergedSaved[k] = v
		}

		historyCount := 0
		savedCount := 0

		if historyPath != "" {
			imported, err := importHistoryCSV(historyPath)
			if err != nil {
				return importDoneMsg{err: fmt.Errorf("reading history CSV: %w", err)}
			}
			for key, entry := range imported {
				if cur, ok := merged[key]; ok {
					if entry.Progress > cur.Progress {
						merged[key] = entry
					}
				} else {
					merged[key] = entry
					historyCount++
				}
			}
		}

		if savedPath != "" {
			imported, err := importSavedCSV(savedPath)
			if err != nil {
				return importDoneMsg{err: fmt.Errorf("reading saved CSV: %w", err)}
			}
			for key, podcast := range imported {
				if _, ok := mergedSaved[key]; !ok {
					mergedSaved[key] = podcast
					savedCount++
				}
			}
		}

		saveHistory(merged)
		saveSaved(mergedSaved)

		return importDoneMsg{
			history:      merged,
			saved:        mergedSaved,
			historyCount: historyCount,
			savedCount:   savedCount,
		}
	}
}
