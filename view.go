package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// renderNowPlayingBar returns a 2-line footer with episode title and progress.
// Returns "" when nothing is playing.
func (m model) renderNowPlayingBar() string {
	if m.pcmStreamer == nil || m.playingTitle == "" {
		return ""
	}

	cur := m.currentPosition()
	var pct float64
	if m.totalDuration > 0 {
		pct = float64(cur) / float64(m.totalDuration)
	}
	timeStr := fmt.Sprintf("%s / %s", formatDur(cur), formatDur(m.totalDuration))

	playIcon := "▶"
	if m.paused {
		playIcon = "⏸"
	}

	toggleKey, playPauseKey := "p", "space"
	if m.state == viewSearch {
		toggleKey, playPauseKey = "ctrl+p", "ctrl+k"
	}
	playPauseIcon := "⏸"
	if m.paused {
		playPauseIcon = "▶"
	}
	hintText := fmt.Sprintf("  ·  %s to toggle  ·  %s %s", toggleKey, playPauseKey, playPauseIcon)

	const sliderWidth = 20
	// 2 (icon+space) + title + 2 (gap) + sliderWidth + 2 (gap) + len(timeStr) + len(hintText)
	titleMaxWidth := max(m.windowWidth-2-sliderWidth-2-len(timeStr)-len(hintText)-4, 8)

	title := m.playingTitle
	runes := []rune(title)
	if len(runes) > titleMaxWidth {
		title = string(runes[:titleMaxWidth-1]) + "…"
	}

	pos := int(pct * float64(sliderWidth))
	slider := fillStyle.Render(strings.Repeat("━", pos)) + barStyle.Render(strings.Repeat("─", max(0, sliderWidth-pos)))
	hint := faintStyle.Render(hintText)
	separator := barStyle.Render(strings.Repeat("─", m.windowWidth))
	infoLine := accentStyle.Render(playIcon+" ") + title + "  " + slider + "  " + faintStyle.Render(timeStr) + hint
	center := lipgloss.NewStyle().Width(m.windowWidth).Align(lipgloss.Center)
	return separator + "\n" + center.Render(infoLine)
}

func (m model) View() string {
	var content string
	switch m.state {
	case viewSearch:
		stats := m.history.computeStats()
		var statsBlock string
		if stats.TotalTime > 0 {
			podcastLine := stats.MostListenedTitle
			maxLen := max(m.windowWidth-22, 10)
			if runes := []rune(podcastLine); len(runes) > maxLen {
				podcastLine = string(runes[:maxLen-1]) + "…"
			}
			statsBlock = "\n\n" + faintStyle.Render("─────────────────────") + "\n" +
				accentStyle.Render("Listening Stats") + "\n\n" +
				faintStyle.Render("Total time:       ") + formatListeningTime(stats.TotalTime) + "\n" +
				faintStyle.Render("Most listened:    ") + podcastLine
		}
		content = fmt.Sprintf("%s\n\n%s\n\n%s%s", titleStyle.Render("PODCAST SEARCH"), m.textInput.View(), faintStyle.Render("Type and press Enter"), statsBlock)
	case viewResults:
		content = titleStyle.Render("Results:") + "\n\n"
		for i, r := range m.searchResults {
			cursor := "  "
			if m.cursor == i {
				cursor = "> "
				content += selStyle.Render(cursor+r.CollectionName) + "\n"
			} else {
				content += cursor + r.CollectionName + "\n"
			}
		}
	case viewEpisodes:
		if m.showEpisodeSearch {
			rows := []string{
				accentStyle.Render("Search Episodes"),
				"",
				m.episodeSearchInput.View(),
				"",
				faintStyle.Render("Enter to search · Esc to cancel"),
			}
			dialog := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("205")).
				Padding(1, 3).
				Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
			content = dialog
			break
		}
		const visibleCount = 15
		scrollTop := 0
		if m.cursor >= visibleCount {
			scrollTop = m.cursor - visibleCount + 1
		}
		const dateColWidth = 12      // "Jan 02, 2006"
		const progressColWidth = 8   // "Progress" header, " 50%    " data
		// Row layout: cursor(2) + "| "(2) + date(12) + " | "(3) + title(N) + " | "(3) + progress(8) + " |"(2) = 32+N
		titleColWidth := max(m.windowWidth-42, 20)
		if titleColWidth > 80 {
			titleColWidth = 80
		}
		header := fmt.Sprintf("  | %-*s | %-*s | %-*s |", dateColWidth, "Date", titleColWidth, "Episode", progressColWidth, "Progress")
		divider := fmt.Sprintf("  |%s|%s|%s|", strings.Repeat("-", dateColWidth+2), strings.Repeat("-", titleColWidth+2), strings.Repeat("-", progressColWidth+2))
		emptyRow := "  " + fmt.Sprintf("| %-*s | %-*s | %-*s |", dateColWidth, "", titleColWidth, "", progressColWidth, "")
		content = titleStyle.Render("Episodes:") + "\n\n"
		if m.episodeFilter != "" {
			suffix := fmt.Sprintf(" · %d result(s) · Esc to clear · s search · m mark · u unmark", len(m.filteredEpisodes))
			// "Filter: " (8) + `"` + query + `"` (2) + suffix
			maxQueryRunes := max(m.windowWidth-8-2-len(suffix), 4)
			displayQuery := m.episodeFilter
			if runes := []rune(displayQuery); len(runes) > maxQueryRunes {
				displayQuery = string(runes[:maxQueryRunes-1]) + "…"
			}
			filterLine := faintStyle.Render("Filter: ") +
				accentStyle.Render("\""+displayQuery+"\"") +
				faintStyle.Render(suffix)
			content += filterLine + "\n"
		} else {
			content += faintStyle.Render("s to search · m mark played · u unmark") + "\n"
		}
		content += faintStyle.Render(header) + "\n"
		content += faintStyle.Render(divider) + "\n"
		rendered := 0
		count := m.episodeCount()
		if count == 0 && m.episodeFilter != "" {
			content += faintStyle.Render("  No episodes match your search.") + "\n"
			rendered = visibleCount // skip empty-row padding
		}
		for i := range count {
			if i < scrollTop || i >= scrollTop+visibleCount {
				continue
			}
			e := m.episodeItemAt(i)
			dateStr := strings.Repeat(" ", dateColWidth)
			if e.PublishedParsed != nil {
				dateStr = e.PublishedParsed.Format("Jan 02, 2006")
			}
			title := e.Title
			if runes := []rune(title); len(runes) > titleColWidth {
				title = string(runes[:titleColWidth-1]) + "…"
			}
			entry, hasHistory := m.history[episodeKey(e)]
			completed := hasHistory && entry.isCompleted()
			inProgress := hasHistory && !completed && entry.Progress > 0

			var progressStr string
			if completed {
				progressStr = "100%"
			} else if inProgress && e.ITunesExt != nil {
				total := parseDuration(e.ITunesExt.Duration)
				if total > 0 {
					pct := int(entry.Progress * 100 / total)
					if pct > 99 {
						pct = 99
					}
					progressStr = fmt.Sprintf("%3d%%", pct)
				}
			}

			row := fmt.Sprintf("| %-*s | %-*s | %-*s |", dateColWidth, dateStr, titleColWidth, title, progressColWidth, progressStr)
			if m.cursor == i {
				content += selStyle.Render("> "+row) + "\n"
			} else if completed {
				content += faintStyle.Render("  "+row) + "\n"
			} else {
				content += "  " + row + "\n"
			}
			rendered++
		}
		for rendered < visibleCount {
			content += emptyRow + "\n"
			rendered++
		}
	case viewPlayer:
		if m.showGoTo {
			rows := []string{
				accentStyle.Render("Go To Position"),
				faintStyle.Render("MM:SS or HH:MM:SS"),
				"",
				m.goToInput.View(),
			}
			if m.goToErr != "" {
				rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F5F")).Render(m.goToErr))
			}
			rows = append(rows, "", faintStyle.Render("Enter confirm · Esc cancel"))
			dialog := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("205")).
				Padding(1, 3).
				Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
			content = dialog
			break
		}
		item := m.feed.Items[m.playingIndex]
		var pct float64
		var cur time.Duration
		if m.pcmStreamer != nil {
			cur = m.currentPosition()
			if m.totalDuration > 0 {
				pct = float64(cur) / float64(m.totalDuration)
			}
		}
		timeStr := fmt.Sprintf("%s / %s", formatDur(cur), formatDur(m.totalDuration))

		var statusLine string
		if m.statusMsg != "" {
			statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F5F")).Render(m.statusMsg)
		} else if m.paused {
			statusLine = faintStyle.Render("- ") + accentStyle.Render("⏸  Paused") + faintStyle.Render(" +")
		} else if m.pcmStreamer != nil && atomic.LoadInt64(&m.pcmStreamer.samplesPlayed) > 0 {
			statusLine = faintStyle.Render("- ") + accentStyle.Render(fmt.Sprintf("%.2fx Speed", m.speed)) + faintStyle.Render(" +")
		} else {
			statusLine = faintStyle.Render("Loading...")
		}

		spaceLabel := "⏸"
		if m.paused {
			spaceLabel = "▶"
		}
		info := lipgloss.JoinVertical(lipgloss.Left,
			accentStyle.Render("▶ NOW PLAYING"),
			titleStyle.Copy().Width(m.windowWidth-50).Render(item.Title),
			"\n", m.renderSlider(pct, m.windowWidth-50, timeStr),
			"\n", statusLine,
			faintStyle.Render(fmt.Sprintf("\n← -10s | → +30s | Space %s | g Go To | Esc ↩", spaceLabel)),
		)
		content = lipgloss.JoinHorizontal(lipgloss.Center, m.albumArt, "    ", info)
	}
	nowPlayingBar := ""
	barHeight := 0
	if m.state != viewPlayer {
		nowPlayingBar = m.renderNowPlayingBar()
		if nowPlayingBar != "" {
			barHeight = 2
		}
	}

	availHeight := max(m.windowHeight-barHeight, 1)
	mainArea := lipgloss.Place(m.windowWidth, availHeight, lipgloss.Center, lipgloss.Center, content)
	if nowPlayingBar != "" {
		return mainArea + "\n" + nowPlayingBar
	}
	return mainArea
}

func (m model) renderSlider(pct float64, width int, timeInfo string) string {
	barWidth := max(width-len(timeInfo)-5, 5)
	pos := int(pct * float64(barWidth))
	return fmt.Sprintf("%s%s%s  %s",
		fillStyle.Render(strings.Repeat("━", pos)),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#FFF")).Render("●"),
		barStyle.Render(strings.Repeat("─", max(0, barWidth-pos))),
		faintStyle.Render(timeInfo),
	)
}
