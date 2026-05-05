package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gopxl/beep/speaker"
	"github.com/mmcdole/gofeed"
)

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	if !m.newEpisodesSince.IsZero() && len(m.savedPodcasts) > 0 {
		for feedURL := range m.savedPodcasts {
			cmds = append(cmds, fetchNewEpisodeCount(feedURL, m.newEpisodesSince))
		}
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowWidth, m.windowHeight = msg.Width, msg.Height
	case SearchResponse:
		m.searchResults = msg.Results
		m.state = viewResults
		m.cursor = 0
	case *gofeed.Feed:
		m.feed = msg
		m.state = viewEpisodes
		m.cursor = 0
		m.filteredEpisodes = nil
		m.episodeFilter = ""
		if p, ok := m.savedPodcasts[m.feedURL]; ok && len(p.Categories) == 0 {
			p.Categories = feedCategories(msg)
			m.savedPodcasts[m.feedURL] = p
			saveSaved(m.savedPodcasts)
		}
		for key, entry := range m.history {
			if entry.FeedURL == m.feedURL && entry.PodcastTitle == "" {
				entry.PodcastTitle = msg.Title
				m.history[key] = entry
			}
		}
		saveHistory(m.history)
	case newEpisodesMsg:
		if msg.count > 0 {
			m.newEpisodeCounts[msg.feedURL] = msg.count
		}
		return m, nil
	case albumArtMsg:
		m.albumArt = string(msg)
	case ffmpegErrMsg:
		if msg.generation == m.ffmpegGeneration && msg.text != "" {
			m.statusMsg = "ffmpeg: " + msg.text
		}
	case exportDoneMsg:
		if msg.err != nil {
			m.exportMsg = "Export failed: " + msg.err.Error()
		} else {
			m.exportMsg = "Exported: " + strings.Join(msg.filenames, ", ")
		}
		return m, nil
	case importDoneMsg:
		if msg.err != nil {
			m.importMsg = "Import failed: " + msg.err.Error()
		} else {
			m.importMsg = fmt.Sprintf("Imported %d history entries, %d saved podcasts", msg.historyCount, msg.savedCount)
			m.history = msg.history
			m.savedPodcasts = msg.saved
		}
		m.showImport = false
		return m, nil
	case seekTimerMsg:
		if msg.seq == m.seekSeq && m.pcmStreamer != nil {
			if m.seekPending {
				// A seek is still in-flight; retry once it has time to finish.
				m.seekSeq++
				seq := m.seekSeq
				target := m.seekOffset
				return m, tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
					return seekTimerMsg{target: target, seq: seq}
				})
			}
			m.seekPending = true
			return m, doSeek(m.pcmStreamer, m.ffmpegCmd, m.currentURL, msg.target, m.speed, m.ffmpegGeneration)
		}
	case seekDoneMsg:
		m.seekPending = false
		if msg.cmd != nil {
			if msg.gen == m.ffmpegGeneration {
				// Valid seek for the current playback session: track the new process.
				if m.ffmpegCmd != nil {
					m.ffmpegCmd.Process.Kill()
					go m.ffmpegCmd.Wait()
				}
				m.ffmpegCmd = msg.cmd
			} else {
				// Don't kill the stale process: the streamer may still be reading
				// from its pipe. It will receive SIGPIPE and exit naturally once
				// the streamer closes the reader during the next swap.
				go msg.cmd.Wait()
			}
		}
	case tickMsg:
		if m.pcmStreamer != nil {
			if m.feed != nil {
				pos := m.currentPosition()
				item := m.feed.Items[m.playingIndex]
				key := episodeKey(item)
				entry := m.history[key]
				entry.Title = item.Title
				entry.FeedURL = m.feedURL
				if m.feed != nil {
					entry.PodcastTitle = m.feed.Title
				}
				entry.Progress = pos
				if m.totalDuration > 0 && pos >= m.totalDuration*95/100 && !entry.isCompleted() {
					entry.Completed = true
					entry.ListenedAt = time.Now()
				}
				m.history[key] = entry
				saveHistory(m.history)
			}
			return m, tick()
		}
	case tea.KeyMsg:
		m.exportMsg = ""
		m.importMsg = ""
		m.listeningStats = m.history.computeStats()
		if m.showGoTo {
			switch msg.String() {
			case "esc":
				m.showGoTo = false
				m.goToErr = ""
				m.goToInput.SetValue("")
			case "enter":
				target, err := parseGoToTime(m.goToInput.Value())
				if err != nil {
					m.goToErr = err.Error()
				} else if m.totalDuration > 0 && target > m.totalDuration {
					m.goToErr = fmt.Sprintf("beyond track end (%s)", formatDur(m.totalDuration))
				} else {
					m.showGoTo = false
					m.goToErr = ""
					m.goToInput.SetValue("")
					m.seekOffset = target
					atomic.StoreInt64(&m.pcmStreamer.samplesPlayed, 0)
					m.seekSeq++
					m.seekPending = true
					return m, doSeek(m.pcmStreamer, m.ffmpegCmd, m.currentURL, target, m.speed, m.ffmpegGeneration)
				}
			default:
				var cmd tea.Cmd
				m.goToInput, cmd = m.goToInput.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if m.showClearHistory {
			switch msg.String() {
			case "esc":
				m.showClearHistory = false
				m.clearHistoryInput.SetValue("")
			case "enter":
				if m.clearHistoryInput.Value() == "delete" {
					m.history = clearHistory()
					m.showClearHistory = false
					m.clearHistoryInput.SetValue("")
				}
			default:
				var cmd tea.Cmd
				m.clearHistoryInput, cmd = m.clearHistoryInput.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if m.showEpisodeSearch {
			switch msg.String() {
			case "esc":
				m.showEpisodeSearch = false
				m.episodeSearchInput.SetValue("")
			case "enter":
				query := strings.TrimSpace(m.episodeSearchInput.Value())
				m.showEpisodeSearch = false
				m.episodeSearchInput.SetValue("")
				if query == "" {
					m.filteredEpisodes = nil
					m.episodeFilter = ""
				} else {
					m.episodeFilter = query
					m.filteredEpisodes = fuzzyFilterEpisodes(m.feed, query)
				}
				m.cursor = 0
			default:
				var cmd tea.Cmd
				m.episodeSearchInput, cmd = m.episodeSearchInput.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if m.showSavedSearch {
			switch msg.String() {
			case "esc":
				m.showSavedSearch = false
				m.savedSearchInput.SetValue("")
			case "enter":
				query := strings.TrimSpace(m.savedSearchInput.Value())
				m.showSavedSearch = false
				m.savedSearchInput.SetValue("")
				if query == "" {
					m.filteredSaved = nil
					m.savedFilter = ""
				} else {
					m.savedFilter = query
					m.filteredSaved = fuzzyFilterSaved(m.savedPodcasts, query)
				}
				m.cursor = 0
			default:
				var cmd tea.Cmd
				m.savedSearchInput, cmd = m.savedSearchInput.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if m.showEpisodeDesc {
			switch msg.String() {
			case "esc":
				m.showEpisodeDesc = false
				m.episodeDescLines = nil
				m.episodeDescScroll = 0
			case "up", "k":
				if m.episodeDescScroll > 0 {
					m.episodeDescScroll--
				}
			case "down", "j":
				maxScroll := max(len(m.episodeDescLines)-descVisibleLines, 0)
				if m.episodeDescScroll < maxScroll {
					m.episodeDescScroll++
				}
			}
			return m, nil
		}
		if m.showImport {
			switch msg.String() {
			case "esc":
				m.showImport = false
			case "tab":
				if m.importFocused == 0 {
					m.importFocused = 1
					m.importHistoryInput.Blur()
					return m, m.importSavedInput.Focus()
				} else {
					m.importFocused = 0
					m.importSavedInput.Blur()
					return m, m.importHistoryInput.Focus()
				}
			case "enter":
				return m, importData(
					m.importHistoryInput.Value(),
					m.importSavedInput.Value(),
					m.history,
					m.savedPodcasts,
				)
			default:
				var cmd tea.Cmd
				if m.importFocused == 0 {
					m.importHistoryInput, cmd = m.importHistoryInput.Update(msg)
				} else {
					m.importSavedInput, cmd = m.importSavedInput.Update(msg)
				}
				return m, cmd
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "q":
			if m.ffmpegCmd != nil {
				m.ffmpegCmd.Process.Kill()
			}
			return m, tea.Quit
		case "esc", "backspace":
			if m.state == viewEpisodes && m.filteredEpisodes != nil {
				m.filteredEpisodes = nil
				m.episodeFilter = ""
				m.cursor = 0
				return m, nil
			}
			if m.state == viewSaved && m.filteredSaved != nil {
				m.filteredSaved = nil
				m.savedFilter = ""
				m.cursor = 0
				return m, nil
			}
			if m.state == viewSaved {
				return m, nil
			}
			if m.state == viewEpisodes && m.fromSaved {
				m.fromSaved = false
				if len(m.savedPodcasts) > 0 {
					m.state = viewSaved
				} else {
					m.state = viewSearch
				}
				m.cursor = 0
				return m, nil
			}
			if m.state > viewSearch {
				m.state--
				return m, nil
			}
			if m.state == viewSearch && msg.String() == "esc" && len(m.savedPodcasts) > 0 {
				m.state = viewSaved
				m.cursor = 0
				return m, nil
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			switch m.state {
			case viewResults:
				if m.cursor < len(m.searchResults)-1 {
					m.cursor++
				}
			case viewEpisodes:
				if m.cursor < m.episodeCount()-1 {
					m.cursor++
				}
			case viewSaved:
				if m.cursor < len(m.savedDisplayURLs())-1 {
					m.cursor++
				}
			default:
				m.cursor++
			}
		case "p":
			if m.state != viewSearch && m.state != viewSaved && m.pcmStreamer != nil {
				m.state = viewPlayer
				return m, nil
			}
		case "ctrl+p":
			if m.pcmStreamer != nil {
				m.state = viewPlayer
				return m, nil
			}
		case "/":
			if m.state == viewSaved {
				m.state = viewSearch
				m.cursor = 0
				return m, m.textInput.Focus()
			}
		case "ctrl+s":
			if m.state == viewEpisodes && m.feed != nil {
				if _, ok := m.savedPodcasts[m.feedURL]; !ok {
					m.savedPodcasts[m.feedURL] = SavedPodcast{
						Title:      m.feed.Title,
						FeedURL:    m.feedURL,
						ArtworkURL: m.artworkURL,
						Categories: feedCategories(m.feed),
					}
					saveSaved(m.savedPodcasts)
				}
				return m, nil
			}
		case "ctrl+u":
			if m.state == viewEpisodes && m.feed != nil {
				if _, ok := m.savedPodcasts[m.feedURL]; ok {
					delete(m.savedPodcasts, m.feedURL)
					saveSaved(m.savedPodcasts)
				}
				return m, nil
			}
		case "ctrl+e":
			if m.state == viewSearch || m.state == viewSaved {
				return m, exportData(m.savedPodcasts, m.history)
			}
		case "ctrl+l":
			if m.state == viewSearch || m.state == viewSaved {
				dir := exportDir()
				m.importHistoryInput.SetValue(findLatestExport(dir, "podcast-tui-history-*.csv"))
				m.importSavedInput.SetValue(findLatestExport(dir, "podcast-tui-saved-*.csv"))
				m.importFocused = 0
				m.showImport = true
				return m, m.importHistoryInput.Focus()
			}
		case "d":
			if m.state == viewEpisodes && m.feed != nil && m.episodeCount() > 0 {
				item := m.episodeItemAt(m.cursor)
				desc := episodeDescription(item)
				innerWidth := max(m.windowWidth-8, 40)
				if innerWidth > 70 {
					innerWidth = 70
				}
				wrapped := lipgloss.NewStyle().Width(innerWidth).Render(desc)
				m.episodeDescLines = strings.Split(wrapped, "\n")
				m.episodeDescScroll = 0
				m.showEpisodeDesc = true
				return m, nil
			}
		case "s":
			if m.state == viewEpisodes {
				m.showEpisodeSearch = true
				m.episodeSearchInput.SetValue("")
				return m, m.episodeSearchInput.Focus()
			} else if m.state == viewSaved {
				m.showSavedSearch = true
				m.savedSearchInput.SetValue("")
				return m, m.savedSearchInput.Focus()
			}
		case "m":
			if m.state == viewEpisodes && m.feed != nil {
				e := m.episodeItemAt(m.cursor)
				key := episodeKey(e)
				entry := m.history[key]
				entry.Title = e.Title
				entry.FeedURL = m.feedURL
				if m.feed != nil {
					entry.PodcastTitle = m.feed.Title
				}
				entry.Completed = true
				entry.ListenedAt = time.Now()
				m.history[key] = entry
				saveHistory(m.history)
			}
		case "u":
			if m.state == viewEpisodes && m.feed != nil {
				e := m.episodeItemAt(m.cursor)
				key := episodeKey(e)
				entry := m.history[key]
				entry.Completed = false
				entry.ListenedAt = time.Time{}
				entry.Progress = 0
				m.history[key] = entry
				saveHistory(m.history)
			}
		case "g":
			if m.state == viewPlayer && m.pcmStreamer != nil {
				m.showGoTo = true
				m.goToErr = ""
				m.goToInput.SetValue("")
				return m, m.goToInput.Focus()
			}
		case " ":
			if m.state != viewSearch && m.ctrl != nil {
				speaker.Lock()
				m.ctrl.Paused = !m.ctrl.Paused
				m.paused = m.ctrl.Paused
				speaker.Unlock()
				return m, nil
			}
		case "ctrl+d":
			if m.state == viewSearch || m.state == viewSaved {
				m.showClearHistory = true
				m.clearHistoryInput.SetValue("")
				return m, m.clearHistoryInput.Focus()
			}
		case "ctrl+k":
			if m.state == viewSearch && m.ctrl != nil {
				speaker.Lock()
				m.ctrl.Paused = !m.ctrl.Paused
				m.paused = m.ctrl.Paused
				speaker.Unlock()
			}
		case "left":
			if m.pcmStreamer != nil {
				target := max(m.currentPosition()-10*time.Second, 0)
				m.seekOffset = target
				atomic.StoreInt64(&m.pcmStreamer.samplesPlayed, 0)
				m.seekSeq++
				seq := m.seekSeq
				return m, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
					return seekTimerMsg{target: target, seq: seq}
				})
			}
		case "right":
			if m.pcmStreamer != nil {
				target := m.currentPosition() + 30*time.Second
				if m.totalDuration > 0 && target > m.totalDuration {
					target = m.totalDuration
				}
				m.seekOffset = target
				atomic.StoreInt64(&m.pcmStreamer.samplesPlayed, 0)
				m.seekSeq++
				seq := m.seekSeq
				return m, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
					return seekTimerMsg{target: target, seq: seq}
				})
			}
		case "-":
			if m.speed > 0.5 && m.pcmStreamer != nil {
				pos := m.currentPosition()
				m.speed -= 0.25
				m.seekOffset = pos
				atomic.StoreInt64(&m.pcmStreamer.samplesPlayed, 0)
				m.seekSeq++
				m.seekPending = false
				m.ffmpegGeneration++
				return m, doSeek(m.pcmStreamer, m.ffmpegCmd, m.currentURL, pos, m.speed, m.ffmpegGeneration)
			}
		case "+", "=":
			if m.speed < 3.0 && m.pcmStreamer != nil {
				pos := m.currentPosition()
				m.speed += 0.25
				m.seekOffset = pos
				atomic.StoreInt64(&m.pcmStreamer.samplesPlayed, 0)
				m.seekSeq++
				m.seekPending = false
				m.ffmpegGeneration++
				return m, doSeek(m.pcmStreamer, m.ffmpegCmd, m.currentURL, pos, m.speed, m.ffmpegGeneration)
			}
		case "enter":
			if m.state == viewSaved {
				urls := m.savedDisplayURLs()
				if m.cursor < len(urls) {
					url := urls[m.cursor]
					podcast := m.savedPodcasts[url]
					m.feedURL = url
					m.artworkURL = podcast.ArtworkURL
					m.fromSaved = true
					return m, tea.Batch(fetchFeed(url), fetchAlbumArt(podcast.ArtworkURL))
				}
			} else if m.state == viewSearch {
				return m, searchPodcasts(m.textInput.Value())
			} else if m.state == viewResults {
				res := m.searchResults[m.cursor]
				m.feedURL = res.FeedURL
				m.artworkURL = res.ArtworkURL
				m.fromSaved = false
				return m, tea.Batch(fetchFeed(res.FeedURL), fetchAlbumArt(res.ArtworkURL))
			} else if m.state == viewEpisodes {
				m.state = viewPlayer
				m.playingIndex = m.feedIndexAt(m.cursor)
				item := m.feed.Items[m.playingIndex]
				m.totalDuration = parseDuration(item.ITunesExt.Duration)
				m.playingTitle = item.Title
				resumeFrom := time.Duration(0)
				if entry, ok := m.history[episodeKey(item)]; ok && !entry.isCompleted() && entry.Progress > 0 {
					resumeFrom = entry.Progress
				}
				return m, m.playAudio(item.Enclosures[0].URL, resumeFrom)
			}
		}
	}
	if m.showGoTo {
		var cmd tea.Cmd
		m.goToInput, cmd = m.goToInput.Update(msg)
		return m, cmd
	}
	if m.state == viewSearch {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
	return m, nil
}
