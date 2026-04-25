package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
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

// --- Audio Tracking ---
type TrackedStreamer struct {
	beep.Streamer
	samplesPlayed int64
}

func (ts *TrackedStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	n, ok = ts.Streamer.Stream(samples)
	ts.samplesPlayed += int64(n)
	return n, ok
}

type tickMsg time.Time

// --- Model ---
type model struct {
	state         int
	textInput     textinput.Model
	searchResults []SearchResult
	feed          *gofeed.Feed
	cursor        int
	albumArt      string
	trackedStream *TrackedStreamer
	resampler     *beep.Resampler
	sampleRate    beep.SampleRate
	totalDuration time.Duration
	speed         float64
	windowWidth   int
	windowHeight  int
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Search for a podcast..."
	ti.Focus()

	return model{
		state:     viewSearch,
		textInput: ti,
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

func (m *model) playAudio(url string) tea.Cmd {
	speaker.Clear()
	resp, _ := http.Get(url)
	streamer, format, _ := mp3.Decode(resp.Body)
	m.sampleRate = format.SampleRate
	m.trackedStream = &TrackedStreamer{Streamer: streamer}
	m.resampler = beep.ResampleRatio(4, m.speed, m.trackedStream)
	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	speaker.Play(m.resampler)
	return tick()
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
	case tickMsg:
		if m.state == viewPlayer {
			return m, tick()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
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
		case "[":
			if m.speed > 0.5 {
				m.speed -= 0.25
				if m.resampler != nil {
					speaker.Lock()
					m.resampler.SetRatio(m.speed)
					speaker.Unlock()
				}
			}
		case "]":
			if m.speed < 3.0 {
				m.speed += 0.25
				if m.resampler != nil {
					speaker.Lock()
					m.resampler.SetRatio(m.speed)
					speaker.Unlock()
				}
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
				return m, m.playAudio(item.Enclosures[0].URL)
			}
		}
	}
	if m.state == viewSearch {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
	return m, nil
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
		item := m.feed.Items[m.cursor]
		var pct float64
		var cur time.Duration
		if m.trackedStream != nil {
			cur = m.sampleRate.D(int(m.trackedStream.samplesPlayed))
			if m.totalDuration > 0 {
				pct = float64(cur) / float64(m.totalDuration)
			}
		}
		timeStr := fmt.Sprintf("%s / %s", formatDur(cur), formatDur(m.totalDuration))

		info := lipgloss.JoinVertical(lipgloss.Left,
			accentStyle.Render("▶ NOW PLAYING"),
			titleStyle.Copy().Width(m.windowWidth-50).Render(item.Title),
			"\n", m.renderSlider(pct, m.windowWidth-50, timeStr),
			"\n", accentStyle.Render(fmt.Sprintf("%.2fx Speed", m.speed)),
			faintStyle.Render("\n[ Slow | ] Fast | Esc Back"),
		)
		content = lipgloss.JoinHorizontal(lipgloss.Center, m.albumArt, "    ", info)
	}
	return lipgloss.Place(m.windowWidth, m.windowHeight, lipgloss.Center, lipgloss.Center, content)
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
