package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	viewHome = iota
	viewSearch
	viewResults
	viewEpisodes
	viewPlayer
	viewSaved
	viewInProgress

	descVisibleLines = 12
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5F00"))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	faintStyle  = lipgloss.NewStyle().Faint(true)
	selStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D7FF")).Bold(true)
	barStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	fillStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D7FF"))
)

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	p.Run()
}
