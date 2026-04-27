package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gopxl/beep/speaker"
	"github.com/mmcdole/gofeed"
)

func (m model) Init() tea.Cmd { return textinput.Blink }

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
	case albumArtMsg:
		m.albumArt = string(msg)
	case ffmpegErrMsg:
		if msg.generation == m.ffmpegGeneration && msg.text != "" {
			m.statusMsg = "ffmpeg: " + msg.text
		}
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
			return m, tick()
		}
	case tea.KeyMsg:
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
					lower := strings.ToLower(query)
					m.filteredEpisodes = []int{}
					for i, item := range m.feed.Items {
						if strings.Contains(strings.ToLower(item.Title), lower) {
							m.filteredEpisodes = append(m.filteredEpisodes, i)
						}
					}
				}
				m.cursor = 0
			default:
				var cmd tea.Cmd
				m.episodeSearchInput, cmd = m.episodeSearchInput.Update(msg)
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
			if m.state > viewSearch {
				m.state--
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
			default:
				m.cursor++
			}
		case "p":
			if m.state != viewSearch && m.pcmStreamer != nil {
				m.state = viewPlayer
				return m, nil
			}
		case "ctrl+p":
			if m.pcmStreamer != nil {
				m.state = viewPlayer
				return m, nil
			}
		case "s":
			if m.state == viewEpisodes {
				m.showEpisodeSearch = true
				m.episodeSearchInput.SetValue("")
				return m, m.episodeSearchInput.Focus()
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
		case "ctrl+ ":
			if m.state == viewSearch && m.ctrl != nil {
				speaker.Lock()
				m.ctrl.Paused = !m.ctrl.Paused
				m.paused = m.ctrl.Paused
				speaker.Unlock()
			}
		case "left":
			if m.pcmStreamer != nil {
				target := m.currentPosition() - 10*time.Second
				if target < 0 {
					target = 0
				}
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
			if m.state == viewSearch {
				return m, searchPodcasts(m.textInput.Value())
			} else if m.state == viewResults {
				res := m.searchResults[m.cursor]
				m.feedURL = res.FeedURL
				return m, tea.Batch(fetchFeed(res.FeedURL), fetchAlbumArt(res.ArtworkURL))
			} else if m.state == viewEpisodes {
				m.state = viewPlayer
				m.playingIndex = m.feedIndexAt(m.cursor)
				item := m.feed.Items[m.playingIndex]
				m.totalDuration = parseDuration(item.ITunesExt.Duration)
				m.playingTitle = item.Title
				m.history[episodeKey(item)] = HistoryEntry{
					Title:      item.Title,
					FeedURL:    m.feedURL,
					ListenedAt: time.Now(),
				}
				saveHistory(m.history)
				return m, m.playAudio(item.Enclosures[0].URL, 0)
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
