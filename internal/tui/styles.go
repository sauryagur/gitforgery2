package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette used across every screen. Colours are 256-colour ANSI indices
// understood by lipgloss in any colour profile.
//
// accentColor is the amber active accent, dangerColor the red used for
// deletions/errors, infoColor the cyan used for hunk headers and informational
// badges, and borderColor the dim border tone that keeps chrome quiet.
var (
	accentColor  = lipgloss.Color("214")
	successColor = lipgloss.Color("42")
	dangerColor  = lipgloss.Color("203")
	infoColor    = lipgloss.Color("81")
	mutedColor   = lipgloss.Color("244")
	borderColor  = lipgloss.Color("238")
	fgColor      = lipgloss.Color("255")
)

var (
	// boxStyle draws a rounded border with no padding, so a box rendered with
	// Width(w-2).Height(h-2) occupies exactly w×h cells.
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())

	panelStyle       = boxStyle.BorderForeground(borderColor)
	panelActiveStyle = boxStyle.BorderForeground(accentColor)

	accentStyle  = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	mutedStyle   = lipgloss.NewStyle().Foreground(mutedColor)
	dangerStyle  = lipgloss.NewStyle().Foreground(dangerColor)
	hunkStyle    = lipgloss.NewStyle().Foreground(infoColor)
	successStyle = lipgloss.NewStyle().Foreground(successColor)

	panelTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	headerStyle     = lipgloss.NewStyle().Bold(true).Foreground(fgColor)
	footerStyle     = lipgloss.NewStyle().Foreground(mutedColor)
	sidebarStyle    = lipgloss.NewStyle().Foreground(fgColor)
	keycapStyle     = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	gutterStyle     = lipgloss.NewStyle().Foreground(borderColor)

	selectedRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(accentColor)
)

// renderBox renders body inside the default rounded border occupying exactly
// w×h cells. title, when non-empty, becomes the first content line.
func renderBox(title, body string, w, h int, active bool) string {
	return renderFrame(title, body, w, h, active)
}

// renderPanel renders body inside a rounded panel occupying exactly w×h cells.
// The border is amber when active and dim grey otherwise, and the title (when
// non-empty) becomes the first content line.
func renderPanel(title, body string, w, h int, active bool) string {
	return renderFrame(title, body, w, h, active)
}

// renderFrame is the shared exact-size box implementation behind renderBox and
// renderPanel. Lines longer than the available content width are truncated and
// the body is padded/trimmed to the available height so the box never
// overflows.
func renderFrame(title, body string, w, h int, active bool) string {
	style := panelStyle
	if active {
		style = panelActiveStyle
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
		lines = append(lines, panelTitleStyle.Render(truncate(title, innerW)))
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
