package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// renderModal draws the modal layer on top of the workspace. It returns the
// empty string when no modal is active so the caller can skip composition.
//
// The panel is centred in the given terminal area. Its width is derived from
// the terminal (clamped to a comfortable reading measure), and its height is
// derived from the rendered content, floored so short prompts still look like
// a real dialog and capped so two rows always remain visible above and below.
// Every body line is clipped to the panel's content width, so long hints or
// harness names can never wrap and break the border.
func renderModal(v workspaceView, w, h int) string {
	if !v.ModalVisible || w <= 0 || h <= 0 {
		return ""
	}

	modalW := clampInt(w-10, 40, 84)
	if modalW > w {
		modalW = w
	}
	contentW := modalW - 2 // one border cell on each side
	if contentW < 1 {
		contentW = 1
	}

	lines := modalBodyLines(v, contentW)

	// One title row plus the two border rows, matching renderPanel.
	modalH := len(lines) + 3
	if modalH < 6 {
		modalH = 6
	}
	if limit := h - 4; modalH > limit {
		modalH = limit
	}
	if modalH < 3 {
		modalH = 3
	}

	// Never attempt to draw more rows than the panel can hold. When a long
	// body has to be clipped, keep the trailing hint (blank separator plus
	// hint line) visible instead of discarding the keys the dialog is asking
	// the user to press.
	if rows := modalH - 3; len(lines) > rows {
		if v.ModalHint != "" && rows >= 2 {
			tail := append([]string(nil), lines[len(lines)-2:]...)
			head := append([]string(nil), lines[:rows-2]...)
			lines = append(head, tail...)
		} else {
			lines = lines[:rows]
		}
	}

	body := strings.Join(lines, "\n")
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
		renderPanel(v.ModalTitle, body, modalW, modalH, true))
}

// modalBodyLines assembles the modal's content rows: the free-form body
// text, the harness picker, the range line and its error, and the key hint.
// All width-dependent styling happens here, where an ANSI-aware lipgloss
// style can be applied to already-clipped plain text.
func modalBodyLines(v workspaceView, contentW int) []string {
	var lines []string

	appendGap := func() {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
	}
	appendText := func(text string) {
		for _, raw := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			lines = append(lines, truncate(raw, contentW))
		}
	}

	if body := strings.TrimRight(v.ModalBody, "\n"); body != "" {
		appendText(body)
	}

	if len(v.Harnesses) > 0 {
		appendGap()
		for i, name := range v.Harnesses {
			row := "  " + name
			if i == v.HarnessIdx {
				row = "▸ " + name
				row = padModalRow(truncate(row, contentW), contentW)
				lines = append(lines, selectedRowStyle.Render(row))
				continue
			}
			lines = append(lines, truncate(row, contentW))
		}
	}

	if v.RangeValue != "" {
		appendGap()
		lines = append(lines, accentStyle.Render(truncate("range: "+v.RangeValue, contentW)))
	}

	if v.RangeErr != "" {
		lines = append(lines, dangerStyle.Render(truncate(v.RangeErr, contentW)))
	}

	if v.ModalHint != "" {
		appendGap()
		lines = append(lines, mutedStyle.Render(truncate(v.ModalHint, contentW)))
	}

	return lines
}

// padModalRow widens s to at least w display cells with spaces so a selected
// row's highlight spans the full panel width. Strings already at or past w
// are returned unchanged.
func padModalRow(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
