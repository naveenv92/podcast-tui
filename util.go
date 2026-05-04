package main

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/sahilm/fuzzy"
)

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	htmlBlockRe  = regexp.MustCompile(`(?i)<(br\s*/?|/?(p|div|li|h[1-6]|blockquote|pre|tr))[^>]*>`)
	multiNewline = regexp.MustCompile(`\n{3,}`)
)

func stripHTML(s string) string {
	s = htmlBlockRe.ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = multiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// episodeDescription returns the best plain-text description for a feed item.
// Priority: ITunesExt.Summary (usually plain text) → Description → Content.
func episodeDescription(item *gofeed.Item) string {
	if item == nil {
		return ""
	}
	if item.ITunesExt != nil && strings.TrimSpace(item.ITunesExt.Summary) != "" {
		return stripHTML(item.ITunesExt.Summary)
	}
	if strings.TrimSpace(item.Description) != "" {
		return stripHTML(item.Description)
	}
	if strings.TrimSpace(item.Content) != "" {
		return stripHTML(item.Content)
	}
	return "No description available."
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

func formatListeningTime(d time.Duration) string {
	totalMinutes := int(d.Minutes())
	minutes := totalMinutes % 60
	totalHours := totalMinutes / 60
	hours := totalHours % 24
	totalDays := totalHours / 24
	days := totalDays % 365
	years := totalDays / 365

	plural := func(n int) string {
		if n == 1 {
			return ""
		}
		return "s"
	}

	var parts []string
	if years > 0 {
		parts = append(parts, fmt.Sprintf("%d year%s", years, plural(years)))
	}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d day%s", days, plural(days)))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour%s", hours, plural(hours)))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d minute%s", minutes, plural(minutes)))
	}
	return strings.Join(parts, ", ")
}

func formatDur(d time.Duration) string {
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// fuzzyFilterEpisodes returns feed indices of items whose title fuzzy-matches
// query, ordered by match score (best first).
func fuzzyFilterEpisodes(feed *gofeed.Feed, query string) []int {
	titles := make([]string, len(feed.Items))
	for i, item := range feed.Items {
		titles[i] = item.Title
	}
	matches := fuzzy.Find(query, titles)
	indices := make([]int, len(matches))
	for i, m := range matches {
		indices[i] = m.Index
	}
	return indices
}

// feedCategories extracts deduplicated category/genre strings from a feed's
// iTunes extension and standard RSS categories.
func feedCategories(feed *gofeed.Feed) []string {
	seen := map[string]bool{}
	var cats []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			cats = append(cats, s)
		}
	}
	if feed.ITunesExt != nil {
		for _, cat := range feed.ITunesExt.Categories {
			if cat == nil {
				continue
			}
			add(cat.Text)
			if cat.Subcategory != nil {
				add(cat.Subcategory.Text)
			}
		}
		for _, kw := range strings.Split(feed.ITunesExt.Keywords, ",") {
			add(kw)
		}
	}
	for _, c := range feed.Categories {
		add(c)
	}
	return cats
}

// fuzzyFilterSaved returns the feed URLs of saved podcasts whose title or
// categories fuzzy-match query, ordered by match score (best first).
func fuzzyFilterSaved(saved SavedPodcasts, query string) []string {
	urls := savedSortedURLs(saved)
	corpus := make([]string, len(urls))
	for i, url := range urls {
		p := saved[url]
		parts := []string{p.Title}
		parts = append(parts, p.Categories...)
		corpus[i] = strings.Join(parts, " ")
	}
	matches := fuzzy.Find(query, corpus)
	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = urls[m.Index]
	}
	return result
}
