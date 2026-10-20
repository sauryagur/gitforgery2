package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// chromeCell is one pre-styled fragment of a single header/footer line. plain
// is the un-styled text used for measurement and clipping; styled is what gets
// emitted when the cell fits. Keeping both lets the row truncate without
// slicing ANSI escapes.
type chromeCell struct {
	plain  string
	styled string
}

func (c chromeCell) width() int { return lipgloss.Width(c.plain) }

func cellText(plain, styled string) chromeCell { return chromeCell{plain: plain, styled: styled} }

func cellsWidth(cells []chromeCell) int {
	total := 0
	for _, c := range cells {
		total += c.width()
	}
	return total
}

// renderCells joins cells, clipping at max cells. A cell that does not fully
// fit is emitted as its plain text so the row never exceeds max.
func renderCells(cells []chromeCell, max int) string {
	var b strings.Builder
	used := 0
	for _, c := range cells {
		if used >= max {
			break
		}
		room := max - used
		if c.width() <= room {
			b.WriteString(c.styled)
			used += c.width()
			continue
		}
		cut := truncate(c.plain, room)
		b.WriteString(cut)
		used += lipgloss.Width(cut)
	}
	return b.String()
}

// composeRow lays out a left-aligned and a right-aligned group of cells on one
// full-width row. Right-hand cells that do not fit are dropped from the end,
// then the left group is clipped, so a narrow terminal degrades gracefully
// instead of wrapping.
func composeRow(left, right []chromeCell, w int) string {
	if w < 1 {
		w = 1
	}
	lw, rw := cellsWidth(left), cellsWidth(right)
	for len(right) > 0 && lw+rw+1 > w {
		right = right[:len(right)-1]
		rw = cellsWidth(right)
	}

	if lw >= w {
		out := renderCells(left, w)
		return out + strings.Repeat(" ", max(0, w-lipgloss.Width(out)))
	}

	room := w - lw - 1
	var rightStr string
	if len(right) > 0 {
		rightStr = renderCells(right, room)
	}
	rw = lipgloss.Width(rightStr)

	leftStr := renderCells(left, lw)
	gap := w - lipgloss.Width(leftStr) - rw
	if gap < 0 {
		gap = 0
	}
	return leftStr + strings.Repeat(" ", gap) + rightStr
}

// renderHeader draws the top chrome line: the brand and branch chip on the
// left, session state (or the help hint) on the right.
func renderHeader(v workspaceView) string {
	w := v.Width
	if w < 1 {
		w = 1
	}

	left := []chromeCell{
		cellText("▣ gitforgery2", accentStyle.Render("▣ gitforgery2")),
	}
	if branch := strings.TrimSpace(v.Branch); branch != "" {
		chip := "  " + branch + " "
		left = append(left, cellText(chip, accentStyle.Render(chip)))
	}

	var right []chromeCell
	if v.HelpVisible {
		right = append(right, cellText("? Help ", mutedStyle.Render("? Help ")))
	} else {
		name := v.Session
		if name == "" {
			name = "Rewrite Session"
		}
		state := "Not Pushed"
		if v.Pushed {
			state = "Pushed"
		}
		label := name + " · " + state
		right = append(right, cellText(
			"✓ "+label+" ",
			successStyle.Render("✓")+" "+mutedStyle.Render(label+" "),
		))
	}

	return composeRow(left, right, w)
}

// footerKey builds a "key label" chrome cell, with the key drawn as a keycap.
func footerKey(key, label string) chromeCell {
	plain := key + " " + label
	return cellText(plain, keycapStyle.Render(key)+" "+mutedStyle.Render(label))
}

// renderFooter draws the bottom key-hint line. Editing and help modes swap in
// their own keys and drop the review hints that no longer apply.
func renderFooter(v workspaceView) string {
	w := v.Width
	if w < 1 {
		w = 1
	}

	left := []chromeCell{
		cellText("? Help", keycapStyle.Render("?")+" "+mutedStyle.Render("Help")),
	}

	var keys []chromeCell
	switch {
	case v.HelpVisible:
		keys = []chromeCell{footerKey("Esc", "Close")}
	case v.EditingPatch:
		keys = []chromeCell{
			footerKey("Ctrl+S", "Save"),
			footerKey("Esc", "Cancel"),
		}
	case v.Step == stepInstruction:
		keys = []chromeCell{
			footerKey("Ctrl+S", "Run"),
			footerKey("Tab", "Toggle Guard"),
			footerKey("Esc", "Back"),
		}
	default:
		keys = []chromeCell{
			footerKey("Enter", "Open"),
			footerKey("e", "Edit"),
			footerKey("o", "Original"),
			footerKey("r", "Reprompt"),
			footerKey("a", "Accept All"),
			footerKey("Esc", "Back"),
			footerKey("q", "Quit"),
		}
	}

	// Space keys apart with a muted separator so the hints stay readable.
	right := make([]chromeCell, 0, len(keys)*2)
	for i, k := range keys {
		if i > 0 {
			right = append(right, cellText("  ", "  "))
		}
		right = append(right, k)
	}

	return composeRow(left, right, w)
}

// helpRow renders one key/description pair for the help overlay.
func helpRow(key, desc string) string {
	return keycapStyle.Render(padCell(key, 7)) + " " + mutedStyle.Render(desc)
}

func padCell(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// renderHelp centres the key-reference overlay over the screen. It returns ""
// when help is not open so callers can splice the result unconditionally.
func renderHelp(v workspaceView, w, h int) string {
	if !v.HelpVisible || w < 8 || h < 6 {
		return ""
	}

	body := strings.Join([]string{
		accentStyle.Render("Global"),
		helpRow("?", "Toggle help"),
		helpRow("q", "Quit"),
		"",
		accentStyle.Render("Review"),
		helpRow("Enter", "Open selected patch"),
		helpRow("e", "Edit instruction"),
		helpRow("o", "Toggle original"),
		helpRow("r", "Reprompt"),
		helpRow("a", "Accept all patches"),
		"",
		accentStyle.Render("While editing"),
		helpRow("Ctrl+S", "Save instruction"),
		helpRow("Esc", "Cancel or close"),
	}, "\n")

	bw := min(max(40, lipgloss.Width(body)+6), w-2)
	bh := min(strings.Count(body, "\n")+3, h-2)
	if bh < 3 {
		bh = 3
	}
	box := renderBox("Help", body, bw, bh, true)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}
