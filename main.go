package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/mmcdole/gofeed"
	"github.com/nfnt/resize"
)

// --- App States ---
const (
	viewSearch = iota
	viewResults
	viewEpisodes
	viewPlayer
)

// --- Styles ---
var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5F00"))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	faintStyle  = lipgloss.NewStyle().Faint(true)
	selStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D7FF")).Bold(true)
	barStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	fillStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D7FF"))
)

// --- Data Structs ---
type SearchResult struct {
	CollectionName string `json:"collectionName"`
	FeedURL        string `json:"feedUrl"`
	ArtworkURL     string `json:"artworkUrl100"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type albumArtMsg string
type ffmpegErrMsg struct {
	generation int
	text       string
}

// swapPayload carries a pre-warmed reader and its pre-buffered audio to PCMStreamer.
type swapPayload struct {
	prebuf []byte
	reader io.ReadCloser
}

// seekDoneMsg is returned when a background seek finishes.
type seekDoneMsg struct {
	cmd *exec.Cmd
	gen int
}

// seekTimerMsg fires after the debounce delay; stale ones are dropped via seq.
type seekTimerMsg struct {
	target time.Duration
	seq    int
}

// --- Audio ---
const outputSampleRate = beep.SampleRate(44100)

// PCMStreamer reads raw s16le stereo PCM from an ffmpeg pipe.
// It supports seamless reader swaps via pendSwap for skip-without-gap seeks.
type PCMStreamer struct {
	reader        io.ReadCloser
	prebuf        []byte
	prebufPos     int
	buf           [4]byte
	samplesPlayed int64 // accessed atomically
	pendSwap      chan swapPayload
}

func (s *PCMStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	// Non-blocking check: swap to pre-warmed reader if one is ready.
	select {
	case sw := <-s.pendSwap:
		s.reader.Close()
		s.reader = sw.reader
		s.prebuf = sw.prebuf
		s.prebufPos = 0
		atomic.StoreInt64(&s.samplesPlayed, 0)
	default:
	}

	for i := range samples {
		var frame [4]byte
		if s.prebufPos < len(s.prebuf) {
			copy(frame[:], s.prebuf[s.prebufPos:s.prebufPos+4])
			s.prebufPos += 4
		} else if _, err := io.ReadFull(s.reader, frame[:]); err != nil {
			return i, i > 0
		}
		samples[i][0] = float64(int16(frame[0])|int16(frame[1])<<8) / 32768.0
		samples[i][1] = float64(int16(frame[2])|int16(frame[3])<<8) / 32768.0
		atomic.AddInt64(&s.samplesPlayed, 1)
	}
	return len(samples), true
}

func (s *PCMStreamer) Err() error { return nil }

// buildAtempoFilter returns an ffmpeg -af value for pitch-preserving speed change.
// atempo only accepts [0.5, 2.0], so values above 2.0 are chained.
func buildAtempoFilter(speed float64) string {
	if speed <= 2.0 {
		return fmt.Sprintf("atempo=%.4f", speed)
	}
	return fmt.Sprintf("atempo=2.0,atempo=%.4f", speed/2.0)
}

// parseGoToTime parses "MM:SS" or "HH:MM:SS" into a duration.
func parseGoToTime(s string) (time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	atoi := func(v string) (int, error) { return strconv.Atoi(v) }
	switch len(parts) {
	case 2:
		m, e1 := atoi(parts[0])
		sec, e2 := atoi(parts[1])
		if e1 != nil || e2 != nil || m < 0 || sec < 0 || sec >= 60 {
			return 0, fmt.Errorf("use MM:SS or HH:MM:SS")
		}
		return time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
	case 3:
		h, e1 := atoi(parts[0])
		m, e2 := atoi(parts[1])
		sec, e3 := atoi(parts[2])
		if e1 != nil || e2 != nil || e3 != nil || h < 0 || m < 0 || m >= 60 || sec < 0 || sec >= 60 {
			return 0, fmt.Errorf("use MM:SS or HH:MM:SS")
		}
		return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
	default:
		return 0, fmt.Errorf("use MM:SS or HH:MM:SS")
	}
}

type tickMsg time.Time

// --- Model ---
type model struct {
	state            int
	textInput        textinput.Model
	searchResults    []SearchResult
	feed             *gofeed.Feed
	cursor           int
	albumArt         string
	pcmStreamer      *PCMStreamer
	ctrl             *beep.Ctrl
	ffmpegCmd        *exec.Cmd
	seekOffset       time.Duration
	currentURL       string
	totalDuration    time.Duration
	speed            float64
	paused           bool
	statusMsg        string
	ffmpegGeneration int
	seekPending      bool // true while a background seek is in-flight
	seekSeq          int  // incremented on each keypress to expire stale debounce timers
	showGoTo         bool
	goToInput        textinput.Model
	goToErr          string
	windowWidth      int
	windowHeight     int
	playingTitle     string
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Search for a podcast..."
	ti.Focus()

	gti := textinput.New()
	gti.Placeholder = "00:00:00"
	gti.CharLimit = 8

	return model{
		state:     viewSearch,
		textInput: ti,
		goToInput: gti,
		speed:     1.0,
	}
}

// --- Commands ---
func searchPodcasts(query string) tea.Cmd {
	return func() tea.Msg {
		endpoint := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=podcast&limit=15", url.QueryEscape(query))
		resp, _ := http.Get(endpoint)
		var res SearchResponse
		json.NewDecoder(resp.Body).Decode(&res)
		return res
	}
}

func fetchFeed(url string) tea.Cmd {
	return func() tea.Msg {
		fp := gofeed.NewParser()
		feed, _ := fp.ParseURL(url)
		return feed
	}
}

func fetchAlbumArt(url string) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(url)
		if err != nil {
			return albumArtMsg("")
		}
		defer resp.Body.Close()

		img, _, err := image.Decode(resp.Body)
		if err != nil {
			return albumArtMsg("")
		}

		// Resize to 32 pixels wide for TUI
		img = resize.Resize(32, 0, img, resize.Lanczos3)
		bounds := img.Bounds()
		var out strings.Builder
		for y := 0; y < bounds.Max.Y; y += 2 {
			for x := 0; x < bounds.Max.X; x++ {
				r1, g1, b1, _ := img.At(x, y).RGBA()
				r2, g2, b2, _ := uint32(0), uint32(0), uint32(0), uint32(0)
				if y+1 < bounds.Max.Y {
					r2, g2, b2, _ = img.At(x, y+1).RGBA()
				}
				fg := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r1>>8, g1>>8, b1>>8))
				bg := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r2>>8, g2>>8, b2>>8))
				out.WriteString(lipgloss.NewStyle().Foreground(fg).Background(bg).Render("▀"))
			}
			out.WriteString("\n")
		}
		return albumArtMsg(out.String())
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

// doSeek starts a new ffmpeg process at target, pre-reads ~93ms into a buffer,
// then queues an atomic reader swap into the live PCMStreamer so there is no
// audio gap. Old audio keeps playing until the swap fires.
func doSeek(pcm *PCMStreamer, oldCmd *exec.Cmd, audioURL string, target time.Duration, speed float64, gen int) tea.Cmd {
	return func() tea.Msg {
		args := []string{"-loglevel", "error"}
		if target > 0 {
			args = append(args, "-ss", fmt.Sprintf("%.3f", target.Seconds()))
		}
		args = append(args,
			"-i", audioURL,
			"-af", buildAtempoFilter(speed),
			"-f", "s16le", "-ar", "44100", "-ac", "2", "pipe:1",
		)
		cmd := exec.Command("ffmpeg", args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return seekDoneMsg{gen: gen}
		}
		if err := cmd.Start(); err != nil {
			return seekDoneMsg{gen: gen}
		}

		// Pre-read ~93ms (4096 frames × 4 bytes) so Stream never starves at swap time.
		prebuf := make([]byte, 4096*4)
		n, _ := io.ReadFull(stdout, prebuf)

		// Drain any stale pending swap before sending ours.
		select {
		case old := <-pcm.pendSwap:
			old.reader.Close()
		default:
		}
		pcm.pendSwap <- swapPayload{prebuf: prebuf[:n], reader: stdout}

		// Kill the process that was playing before this seek started.
		if oldCmd != nil {
			oldCmd.Process.Kill()
			go oldCmd.Wait()
		}

		return seekDoneMsg{cmd: cmd, gen: gen}
	}
}

func tick() tea.Cmd {
	return tea.Every(time.Millisecond*500, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// --- Update ---
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
				// Superseded by a speed change: kill the orphaned process.
				msg.cmd.Process.Kill()
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
		switch msg.String() {
		case "ctrl+c", "q":
			if m.ffmpegCmd != nil {
				m.ffmpegCmd.Process.Kill()
			}
			return m, tea.Quit
		case "esc", "backspace":
			if m.state > viewSearch {
				m.state--
				return m, nil
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			m.cursor++
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
				return m, tea.Batch(fetchFeed(res.FeedURL), fetchAlbumArt(res.ArtworkURL))
			} else if m.state == viewEpisodes {
				m.state = viewPlayer
				item := m.feed.Items[m.cursor]
				m.totalDuration = parseDuration(item.ITunesExt.Duration)
				m.playingTitle = item.Title
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

	toggleKey, spaceKey := "p", "space"
	if m.state == viewSearch {
		toggleKey, spaceKey = "ctrl+p", "ctrl+space"
	}
	spaceIcon := "⏸"
	if m.paused {
		spaceIcon = "▶"
	}
	hintText := fmt.Sprintf("  ·  %s to toggle  ·  %s %s", toggleKey, spaceKey, spaceIcon)

	const sliderWidth = 20
	// 2 (icon+space) + title + 2 (gap) + sliderWidth + 2 (gap) + len(timeStr) + len(hintText)
	titleMaxWidth := m.windowWidth - 2 - sliderWidth - 2 - len(timeStr) - len(hintText) - 4
	if titleMaxWidth < 8 {
		titleMaxWidth = 8
	}

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

// --- View ---
func (m model) View() string {
	var content string
	switch m.state {
	case viewSearch:
		content = fmt.Sprintf("%s\n\n%s\n\n%s", titleStyle.Render("PODCAST SEARCH"), m.textInput.View(), faintStyle.Render("Type and press Enter"))
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
		content = titleStyle.Render("Episodes:") + "\n\n"
		for i, e := range m.feed.Items {
			if i > 12 {
				break
			}
			cursor := "  "
			if m.cursor == i {
				cursor = "> "
				content += selStyle.Render(cursor+e.Title) + "\n"
			} else {
				content += cursor + e.Title + "\n"
			}
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
		item := m.feed.Items[m.cursor]
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

	availHeight := m.windowHeight - barHeight
	if availHeight < 1 {
		availHeight = 1
	}
	mainArea := lipgloss.Place(m.windowWidth, availHeight, lipgloss.Center, lipgloss.Center, content)
	if nowPlayingBar != "" {
		return mainArea + "\n" + nowPlayingBar
	}
	return mainArea
}

func (m model) renderSlider(pct float64, width int, timeInfo string) string {
	barWidth := width - len(timeInfo) - 5
	if barWidth < 5 {
		barWidth = 5
	}
	pos := int(pct * float64(barWidth))
	return fmt.Sprintf("%s%s%s  %s", fillStyle.Render(strings.Repeat("━", pos)), lipgloss.NewStyle().Foreground(lipgloss.Color("#FFF")).Render("●"), barStyle.Render(strings.Repeat("─", max(0, barWidth-pos))), faintStyle.Render(timeInfo))
}

func formatDur(d time.Duration) string {
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func parseDuration(s string) time.Duration {
	p := strings.Split(s, ":")
	var d time.Duration
	m := []time.Duration{time.Second, time.Minute, time.Hour}
	for i := 0; i < len(p) && i < 3; i++ {
		v, _ := strconv.Atoi(p[len(p)-1-i])
		d += time.Duration(v) * m[i]
	}
	return d
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	p.Run()
}
