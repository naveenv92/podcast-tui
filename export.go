package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type exportDoneMsg struct {
	filename string
	err      error
}

func exportData(saved SavedPodcasts, history History) tea.Cmd {
	return func() tea.Msg {
		filename := "podcast-tui-export-" + time.Now().Format("2006-01-02") + ".md"
		content := buildMarkdown(saved, history)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			return exportDoneMsg{err: err}
		}
		return exportDoneMsg{filename: filename}
	}
}

func buildMarkdown(saved SavedPodcasts, history History) string {
	var sb strings.Builder

	sb.WriteString("# Podcast TUI Export\n\n")
	sb.WriteString("_Generated: " + time.Now().Format("January 2, 2006 at 3:04 PM") + "_\n\n")
	sb.WriteString("---\n\n")

	sb.WriteString("## Saved Podcasts\n\n")
	if len(saved) == 0 {
		sb.WriteString("_No saved podcasts._\n\n")
	} else {
		for _, url := range savedSortedURLs(saved) {
			p := saved[url]
			sb.WriteString("### " + p.Title + "\n\n")
			sb.WriteString("- **Feed URL:** " + p.FeedURL + "\n")
			if p.ArtworkURL != "" {
				sb.WriteString("- **Artwork:** " + p.ArtworkURL + "\n")
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("---\n\n")

	sb.WriteString("## Listening History\n\n")
	if len(history) == 0 {
		sb.WriteString("_No listening history._\n\n")
	} else {
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
			sb.WriteString("### " + e.Title + "\n\n")
			sb.WriteString("- **Podcast:** " + e.PodcastTitle + "\n")
			sb.WriteString("- **Progress:** " + formatExportDuration(e.Progress) + "\n")
			if e.Completed {
				sb.WriteString("- **Status:** Completed\n")
			} else {
				sb.WriteString("- **Status:** In Progress\n")
			}
			if !e.ListenedAt.IsZero() {
				sb.WriteString("- **Last listened:** " + e.ListenedAt.Format("January 2, 2006 at 3:04 PM") + "\n")
			}
			if e.FeedURL != "" {
				sb.WriteString("- **Feed URL:** " + e.FeedURL + "\n")
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func formatExportDuration(d time.Duration) string {
	if d <= 0 {
		return "0m 0s"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}
