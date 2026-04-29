package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

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
