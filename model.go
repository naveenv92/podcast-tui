package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/mmcdole/gofeed"
)

type model struct {
	state            int
	textInput        textinput.Model
	searchResults    []SearchResult
	feed             *gofeed.Feed
	feedURL          string
	cursor           int
	albumArt         string
	history          History
	pcmStreamer      *PCMStreamer
	ctrl             *beep.Ctrl
	ffmpegCmd        *exec.Cmd
	seekOffset       time.Duration
	currentURL       string
	totalDuration    time.Duration
	speed            float64
	paused           bool
	statusMsg        string
	exportMsg        string
	ffmpegGeneration int
	seekPending      bool // true while a background seek is in-flight
	seekSeq          int  // incremented on each keypress to expire stale debounce timers
	statsTick        int  // counts playback ticks for 30s stats refresh
	showGoTo         bool
	goToInput        textinput.Model
	goToErr          string
	windowWidth      int
	windowHeight     int
	playingTitle       string
	playingIndex       int    // feed index of the episode currently in the player
	playingAlbumArt    string // album art snapshot at play time
	playingFeedURL     string // feed URL snapshot at play time
	playingPodcastTitle string // podcast title snapshot at play time
	playingEpisodeKey  string // history key snapshot at play time
	showClearHistory   bool
	clearHistoryInput  textinput.Model
	// episode search / filter
	showEpisodeSearch  bool
	episodeSearchInput textarea.Model
	episodeFilter      string // raw query shown in UI; empty = no filter
	filteredEpisodes   []int  // feed indices matching the filter; nil = no filter
	// saved podcast filter
	showSavedSearch  bool
	savedSearchInput textinput.Model
	savedFilter      string   // raw query; empty = no filter
	filteredSaved    []string // feed URLs matching the filter; nil = no filter
	// episode description modal
	showEpisodeDesc   bool
	episodeDescLines  []string
	episodeDescScroll int
	// saved podcasts
	savedPodcasts   SavedPodcasts
	fromSaved       bool   // true when viewEpisodes was reached from viewSaved
	artworkURL      string // artwork URL of the currently loaded podcast
	newEpisodeCounts map[string]int // feedURL -> count of episodes since last open
	newEpisodesSince time.Time      // lastOpenedAt from previous session (zero = first run)
	listeningStats   ListeningStats
	// import
	showImport         bool
	importHistoryInput textinput.Model
	importSavedInput   textinput.Model
	importFocused      int
	importMsg          string
}

// episodeCount returns the number of episodes currently displayed (filtered or all).
func (m model) episodeCount() int {
	if m.filteredEpisodes != nil {
		return len(m.filteredEpisodes)
	}
	if m.feed != nil {
		return len(m.feed.Items)
	}
	return 0
}

// episodeItemAt returns the feed item at display position i.
func (m model) episodeItemAt(i int) *gofeed.Item {
	if m.filteredEpisodes != nil {
		return m.feed.Items[m.filteredEpisodes[i]]
	}
	return m.feed.Items[i]
}

// savedDisplayURLs returns the ordered feed URLs for the saved podcast list,
// respecting any active filter.
func (m model) savedDisplayURLs() []string {
	if m.filteredSaved != nil {
		return m.filteredSaved
	}
	return savedSortedURLs(m.savedPodcasts)
}

// feedIndexAt converts a display-list index to the underlying feed index.
func (m model) feedIndexAt(i int) int {
	if m.filteredEpisodes != nil {
		return m.filteredEpisodes[i]
	}
	return i
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Search for a podcast..."
	ti.Focus()

	gti := textinput.New()
	gti.Placeholder = "00:00:00"
	gti.CharLimit = 8

	chi := textinput.New()
	chi.Placeholder = "delete"
	chi.CharLimit = 6

	ssi := textinput.New()
	ssi.Placeholder = "Search by title or genre..."
	ssi.CharLimit = 100

	ihp := textinput.New()
	ihp.Placeholder = "Path to history CSV (leave blank to skip)"
	ihp.CharLimit = 512

	isp := textinput.New()
	isp.Placeholder = "Path to saved podcasts CSV (leave blank to skip)"
	isp.CharLimit = 512

	esi := textarea.New()
	esi.Prompt = ""    // must be set before SetWidth so prompt width is 0
	esi.Placeholder = "" // avoid placeholderView(), which doesn't pad lines to width
	esi.SetWidth(40)
	esi.SetHeight(3)
	esi.ShowLineNumbers = false
	esi.CharLimit = 200
	esi.FocusedStyle.Base = lipgloss.NewStyle()
	esi.BlurredStyle.Base = lipgloss.NewStyle()
	esi.FocusedStyle.CursorLine = lipgloss.NewStyle()

	saved := loadSaved()
	initialState := viewSearch
	if len(saved) > 0 {
		initialState = viewSaved
	}

	meta := loadMeta()
	saveMeta(AppMeta{LastOpenedAt: time.Now()})

	return model{
		state:              initialState,
		textInput:          ti,
		goToInput:          gti,
		clearHistoryInput:  chi,
		episodeSearchInput: esi,
		savedSearchInput:   ssi,
		speed:              1.0,
		history:            loadHistory(),
		savedPodcasts:      saved,
		newEpisodeCounts:   make(map[string]int),
		newEpisodesSince:   meta.LastOpenedAt,
		listeningStats:     loadHistory().computeStats(),
		importHistoryInput: ihp,
		importSavedInput:   isp,
	}
}

func (m *model) currentPosition() time.Duration {
	if m.pcmStreamer == nil {
		return m.seekOffset
	}
	elapsed := time.Duration(atomic.LoadInt64(&m.pcmStreamer.samplesPlayed)) * time.Second / time.Duration(outputSampleRate)
	return m.seekOffset + time.Duration(float64(elapsed)*m.speed)
}

func (m *model) playAudio(audioURL string, seekTo time.Duration) tea.Cmd {
	speaker.Clear()
	if m.ffmpegCmd != nil {
		m.ffmpegCmd.Process.Kill()
		m.ffmpegCmd.Wait()
	}
	// Drain any in-flight seek payload so its reader gets closed.
	if m.pcmStreamer != nil {
		select {
		case old := <-m.pcmStreamer.pendSwap:
			old.reader.Close()
		default:
		}
	}

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		m.statusMsg = "ffmpeg not found — install with: brew install ffmpeg"
		return tick()
	}

	args := []string{"-loglevel", "error"}
	if seekTo > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", seekTo.Seconds()))
	}
	args = append(args,
		"-i", audioURL,
		"-af", buildAtempoFilter(m.speed),
		"-f", "s16le",
		"-ar", "44100",
		"-ac", "2",
		"pipe:1",
	)

	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.statusMsg = fmt.Sprintf("ffmpeg pipe error: %v", err)
		return tick()
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.statusMsg = fmt.Sprintf("ffmpeg pipe error: %v", err)
		return tick()
	}
	if err := cmd.Start(); err != nil {
		m.statusMsg = fmt.Sprintf("ffmpeg failed to start: %v", err)
		return tick()
	}

	m.ffmpegCmd = cmd
	m.pcmStreamer = &PCMStreamer{reader: stdout, pendSwap: make(chan swapPayload, 1)}
	m.ctrl = &beep.Ctrl{Streamer: m.pcmStreamer}
	m.seekOffset = seekTo
	m.currentURL = audioURL
	m.statusMsg = ""
	m.paused = false
	m.seekPending = false
	m.seekSeq++
	m.ffmpegGeneration++
	gen := m.ffmpegGeneration

	speaker.Init(outputSampleRate, outputSampleRate.N(time.Second/10))
	speaker.Play(m.ctrl)

	stderrCmd := func() tea.Msg {
		data, _ := io.ReadAll(stderr)
		return ffmpegErrMsg{generation: gen, text: strings.TrimSpace(string(data))}
	}
	return tea.Batch(tick(), stderrCmd)
}
