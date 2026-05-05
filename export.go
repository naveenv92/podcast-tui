package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type exportDoneMsg struct {
	filenames []string
	err       error
}

func exportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	downloads := filepath.Join(home, "Downloads")
	if info, err := os.Stat(downloads); err == nil && info.IsDir() {
		return downloads
	}
	return home
}

func exportData(saved SavedPodcasts, history History) tea.Cmd {
	return func() tea.Msg {
		dir := exportDir()
		date := time.Now().Format("2006-01-02")
		histFile := filepath.Join(dir, "podcast-tui-history-"+date+".csv")
		savedFile := filepath.Join(dir, "podcast-tui-saved-"+date+".csv")

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
	w.Write([]string{"Episode", "Podcast", "Status", "Progress (seconds)", "Last Listened", "Feed URL", "Key"})

	type historyRow struct {
		key   string
		entry HistoryEntry
	}
	rows := make([]historyRow, 0, len(history))
	for k, e := range history {
		rows = append(rows, historyRow{k, e})
	}
	sort.Slice(rows, func(i, j int) bool {
		ti, tj := rows[i].entry.ListenedAt, rows[j].entry.ListenedAt
		if ti.IsZero() && tj.IsZero() {
			return rows[i].entry.Title < rows[j].entry.Title
		}
		if ti.IsZero() {
			return false
		}
		if tj.IsZero() {
			return true
		}
		return ti.After(tj)
	})

	for _, row := range rows {
		e := row.entry
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
			row.key,
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

