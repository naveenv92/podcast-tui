package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type exportDoneMsg struct {
	filenames []string
	err       error
}

func exportData(saved SavedPodcasts, history History) tea.Cmd {
	return func() tea.Msg {
		date := time.Now().Format("2006-01-02")
		histFile := "podcast-tui-history-" + date + ".csv"
		savedFile := "podcast-tui-saved-" + date + ".csv"

		if err := writeHistoryCSV(histFile, history); err != nil {
			return exportDoneMsg{err: err}
		}
		if err := writeSavedCSV(savedFile, saved); err != nil {
			return exportDoneMsg{err: err}
		}
		return exportDoneMsg{filenames: []string{histFile, savedFile}}
	}
}

func writeHistoryCSV(filename string, history History) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"Episode", "Podcast", "Status", "Progress (seconds)", "Last Listened", "Feed URL"})

	entries := make([]HistoryEntry, 0, len(history))
	for _, e := range history {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		ti, tj := entries[i].ListenedAt, entries[j].ListenedAt
		if ti.IsZero() && tj.IsZero() {
			return entries[i].Title < entries[j].Title
		}
		if ti.IsZero() {
			return false
		}
		if tj.IsZero() {
			return true
		}
		return ti.After(tj)
	})

	for _, e := range entries {
		status := "In Progress"
		if e.isCompleted() {
			status = "Completed"
		}
		listenedAt := ""
		if !e.ListenedAt.IsZero() {
			listenedAt = e.ListenedAt.Format("2006-01-02 15:04:05")
		}
		w.Write([]string{
			e.Title,
			e.PodcastTitle,
			status,
			fmt.Sprintf("%d", int(e.Progress.Seconds())),
			listenedAt,
			e.FeedURL,
		})
	}

	w.Flush()
	return w.Error()
}

func writeSavedCSV(filename string, saved SavedPodcasts) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"Title", "Feed URL", "Artwork URL", "Categories"})

	for _, url := range savedSortedURLs(saved) {
		p := saved[url]
		categories := strings.Join(p.Categories, "|")
		w.Write([]string{p.Title, p.FeedURL, p.ArtworkURL, categories})
	}

	w.Flush()
	return w.Error()
}

