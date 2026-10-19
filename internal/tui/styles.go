package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette used across every screen. Colours are 256-colour ANSI indices
// understood by lipgloss in any colour profile.
var (
	accentColor  = lipgloss.Color("63")
	mutedColor   = lipgloss.Color("241")
	errorColor   = lipgloss.Color("203")
	successColor = lipgloss.Color("42")
)

var (
	// boxStyle draws a rounded border with no padding, so a box rendered with
	// Width(w-2).Height(h-2) occupies exactly w×h cells.
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())

	activePaneStyle   = boxStyle.BorderForeground(accentColor)
	inactivePaneStyle = boxStyle.BorderForeground(mutedColor)

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	helpStyle  = lipgloss.NewStyle().Foreground(mutedColor)
	// statusBarStyle is muted; its width is set per render so the bar spans the
	// full terminal width.
	statusBarStyle = lipgloss.NewStyle().Foreground(mutedColor)

	selectedItemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(accentColor)

	errorStyle   = lipgloss.NewStyle().Foreground(errorColor)
	successStyle = lipgloss.NewStyle().Foreground(successColor)
)

// renderBox renders body inside a rounded border occupying exactly w×h cells.
// title, when non-empty, becomes the first content line. Lines longer than the
// available content width are truncated and the body is padded/trimmed to the
// available height so the box never overflows.
func renderBox(title, body string, w, h int, active bool) string {
	style := inactivePaneStyle
	if active {
		style = activePaneStyle
	}
	if w < 4 {
		w = 4
	}
	if h < 3 {
		h = 3
	}
	innerW, innerH := w-2, h-2

	var lines []string
	if title != "" {
		lines = append(lines, titleStyle.Render(truncate(title, innerW)))
	}
	for _, line := range strings.Split(body, "\n") {
		if lipgloss.Width(line) > innerW {
			line = truncate(line, innerW)
		}
		lines = append(lines, line)
	}
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")
	return style.Width(innerW).Height(innerH).Render(content)
}

// renderStatusBar renders a single full-width muted status line. The text is
// clipped to w cells before styling so it never wraps onto a second line.
func renderStatusBar(w int, text string) string {
	if w < 1 {
		w = 1
	}
	return statusBarStyle.Width(w).Render(truncate(text, w))
}

// truncate shortens s to at most w display cells. It walks whole runes (never
// splitting a multi-byte character) and is not ANSI-aware, which is sufficient
// for the plain-text content this TUI renders (labels, hints, patch text).
// Clipping by cell width rather than rune count keeps wide runes (CJK, emoji)
// from silently overflowing a fixed-size box.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		cw := lipgloss.Width(string(r))
		if cw < 1 {
			cw = 1
		}
		if used+cw > w {
			break
		}
		b.WriteRune(r)
		used += cw
	}
	return b.String()
}
