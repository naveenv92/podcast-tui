package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/gofeed"
	"github.com/nfnt/resize"
)

type SearchResult struct {
	CollectionName string `json:"collectionName"`
	FeedURL        string `json:"feedUrl"`
	ArtworkURL     string `json:"artworkUrl100"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type albumArtMsg string

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
