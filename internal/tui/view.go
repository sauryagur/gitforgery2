package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) viewContent() string {
	switch m.screen {
	case scrSetup:
		var b strings.Builder
		if len(m.available) == 0 {
			b.WriteString("No harness found (pi, codex, claude). Install one and re-run.")
		} else {
			for i, h := range m.available {
				if i > 0 {
					b.WriteString("\n")
				}
				if i == m.setupIdx {
					b.WriteString(selectedItemStyle.Render("> " + h))
				} else {
					b.WriteString("  " + h)
				}
			}
		}
		return m.renderForm("Select harness", b.String(), "Enter select · Ctrl+C quit")
	case scrRange:
		body := m.rangeInput.View()
		if m.rangeErr != "" {
			body = errorStyle.Render(m.rangeErr) + "\n\n" + body
		}
		return m.renderForm("Commit range", body, "Enter continue")
	case scrGenerate:
		return m.renderForm("Generating instruction", m.outputViewport.View(), m.spinner.View()+" "+m.instructionStatus+" · esc skip")
	case scrInstruction:
		var b strings.Builder
		if m.instructionStatus != "" {
			b.WriteString(helpStyle.Render(m.instructionStatus) + "\n\n")
		}
		b.WriteString(m.instrInput.View())
		b.WriteString("\n\nTab: don't modify final state = " + boolStr(m.dontModify))
		return m.renderForm("Instruction", b.String(), "Enter run")
	case scrRun:
		return m.renderForm("Running", m.outputViewport.View(), m.spinner.View()+" "+m.runStatus+" · esc cancel")
	case scrReview:
		return m.renderReview()
	case scrConfirm:
		if m.pushing {
			return m.renderForm("Confirm force-push", "Pushing...", "please wait")
		}
		var b strings.Builder
		b.WriteString("Force-push the rewritten history?")
		_, _ = fmt.Fprintf(&b, "\n\n%d patch(es) will be applied", len(m.patches))
		hint := "y/n"
		switch {
		case m.pushErr != "":
			_, _ = fmt.Fprintf(&b, "\n\ntarget unavailable: %s", m.pushErr)
			hint = "n/q abort"
		case m.pushRemote != "" || m.pushBranch != "":
			_, _ = fmt.Fprintf(&b, "\n\ntarget: %s/%s", m.pushRemote, m.pushBranch)
		}
		return m.renderForm("Confirm force-push", b.String(), hint)
	case scrDone:
		if m.finalErr != nil {
			return m.renderForm("Error", errorStyle.Render(m.finalMsg), "Enter/q exit")
		}
		return m.renderForm("Done", successStyle.Render(m.finalMsg), "Enter/q exit")
	}
	return ""
}

// renderForm centres a bordered box holding a title, body and key hint at the
// current terminal size. Used by every screen except review.
func (m Model) renderForm(title, body, hint string) string {
	w, h := m.dims()
	content := body
	if hint != "" {
		if content != "" {
			content += "\n\n"
		}
		content += helpStyle.Render(hint)
	}
	lines := 0
	if content != "" {
		lines = strings.Count(content, "\n") + 1
	}
	bh := lines + 3 // one title line plus two border rows
	if bh > h {
		bh = h
	}
	if bh < 3 {
		bh = 3
	}
	box := renderBox(title, content, m.formBoxWidth(), bh, true)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

// renderReview draws the lazygit-style review layout: the patch list on the
// left, the diff viewport (or the editor while editing) on the right, and the
// key hints in a full-width status bar along the bottom.
func (m Model) renderReview() string {
	cw, ch := m.dims()
	bodyH := ch - 1 // bottom status bar
	if bodyH < 3 {
		bodyH = 3
	}
	lw, rw := m.reviewPaneWidths()

	innerLW := lw - 2
	if innerLW < 1 {
		innerLW = 1
	}
	visible := bodyH - 3 // borders plus title row
	if visible < 1 {
		visible = 1
	}
	// patchNames is populated when patches are produced; fall back to file
	// names if a caller left it unset so the list is never blank.
	labels := m.patchNames
	if len(labels) != len(m.patches) {
		labels = make([]string, len(m.patches))
		for i, p := range m.patches {
			labels[i] = filepath.Base(p)
		}
	}
	start := 0
	if m.selected >= visible {
		start = m.selected - visible + 1
	}
	if maxStart := len(labels) - visible; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	var list strings.Builder
	for i := start; i < len(labels) && i < start+visible; i++ {
		if i == m.selected {
			list.WriteString(selectedItemStyle.Width(innerLW).Render(truncate("> "+labels[i], innerLW)))
		} else {
			list.WriteString(truncate("  "+labels[i], innerLW))
		}
		list.WriteString("\n")
	}
	left := renderBox("Patches", strings.TrimRight(list.String(), "\n"), lw, bodyH, !m.editing)

	title := "Patch"
	body := m.viewport.View()
	if m.editing {
		title = "Edit patch"
		body = m.textarea.View()
	}
	right := renderBox(title, body, rw, bodyH, m.editing)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	hint := "↑/↓ select  e edit  r reprompt  a accept  q abort"
	if m.editing {
		hint = "Ctrl+S save  Esc cancel"
	}
	if m.reviewStatus != "" {
		hint += "  |  " + m.reviewStatus
	}
	return panes + "\n" + renderStatusBar(cw, hint)
}
