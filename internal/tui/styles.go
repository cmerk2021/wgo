package tui

import "github.com/charmbracelet/lipgloss"

var (
	accent = lipgloss.Color("42")  // green accent
	warn   = lipgloss.Color("214") // amber
	danger = lipgloss.Color("196") // red
	dim    = lipgloss.Color("240")
	bright = lipgloss.Color("252")

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(bright)

	styleTabActive = lipgloss.NewStyle().Bold(true).Foreground(accent).
			Padding(0, 2).Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(accent)
	styleTab = lipgloss.NewStyle().Foreground(dim).Padding(0, 2)

	styleConnected    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	styleConnecting   = lipgloss.NewStyle().Bold(true).Foreground(warn)
	styleDisconnected = lipgloss.NewStyle().Bold(true).Foreground(dim)
	styleError        = lipgloss.NewStyle().Foreground(danger)
	styleStale        = lipgloss.NewStyle().Bold(true).Foreground(warn)

	styleLabel = lipgloss.NewStyle().Foreground(dim)
	styleValue = lipgloss.NewStyle().Foreground(bright)
	styleOK    = lipgloss.NewStyle().Foreground(accent)
	styleFail  = lipgloss.NewStyle().Foreground(danger)

	styleFooter = lipgloss.NewStyle().Foreground(dim)
	styleBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(dim).Padding(0, 1)
	styleHelp = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).Padding(1, 2)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(accent)
)
