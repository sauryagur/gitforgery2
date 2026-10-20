package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderInstruction renders the instruction editor inside a titled panel. Each
// instruction line gets a right-aligned line-number gutter; the cursor line at
// v.InstructionLine is highlighted, a block cursor is appended while editing,
// and any error is shown in danger colours on the last row.
func renderInstruction(v workspaceView, w, h int) string {
	if w < 4 {
		w = 4
	}
	if h < 3 {
		h = 3
	}
	innerW := w - 2
	// One border row on each side plus the panel title row.
	maxLines := h - 3
	if maxLines < 1 {
		maxLines = 1
	}

	lines := strings.Split(v.Instruction, "\n")
	cur := v.InstructionLine
	if cur < 0 {
		cur = 0
	}
	if cur >= len(lines) {
		cur = len(lines) - 1
	}

	// Reserve the final body row for the error so it stays visible.
	if v.InstructionErr != "" && maxLines > 1 {
		maxLines--
	}

	numW := len(fmt.Sprint(len(lines)))
	if numW < 2 {
		numW = 2
	}
	gutterW := numW + 1 // line number plus separating space
	if gutterW > innerW-1 {
		gutterW = innerW - 1
	}
	if gutterW < 0 {
		gutterW = 0
	}
	textW := innerW - gutterW

	// Scroll just enough to keep the cursor line on screen.
	start := 0
	if len(lines) > maxLines {
		start = cur - maxLines + 1
		if start < 0 {
			start = 0
		}
	}
	end := start + maxLines
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		if b.Len() > 0 {
			b.WriteString("\n")
		}

		num := fmt.Sprintf("%*d ", numW, i+1)
		if gutterW < lipgloss.Width(num) {
			num = truncate(num, gutterW)
		}
		if gutterW > 0 {
			b.WriteString(gutterStyle.Render(num))
		}

		text := truncate(lines[i], textW)
		if v.InstructionEditing && i == cur {
			text = truncate(text, textW-1) + "█"
		}
		if i == cur {
			b.WriteString(selectedRowStyle.Width(textW).Render(text))
		} else {
			b.WriteString(text)
		}
	}

	if v.InstructionErr != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(dangerStyle.Render(truncate("! "+v.InstructionErr, innerW)))
	}

	return renderPanel("Instruction", b.String(), w, h, v.Step == stepInstruction)
}

// renderContext renders the run context summary: harness, commit range, patch
// count and whether the rewritten tree must match the original.
func renderContext(v workspaceView, w, h int) string {
	if w < 4 {
		w = 4
	}
	if h < 3 {
		h = 3
	}
	innerW := w - 2

	harness := v.Harness
	if harness == "" {
		harness = "—"
	}
	rangeDesc := v.RangeDesc
	if rangeDesc == "" {
		rangeDesc = "—"
	}

	treeValue, treeStyle := "May change", dangerStyle
	if v.FinalTreeMatch {
		treeValue, treeStyle = "Must match", successStyle
	}

	rows := []struct {
		label string
		value string
		style lipgloss.Style
	}{
		{"Harness", harness, lipgloss.NewStyle()},
		{"Range", rangeDesc, lipgloss.NewStyle()},
		{"Patches", fmt.Sprintf("%d", v.PatchCount), lipgloss.NewStyle()},
		{"Final Tree", treeValue, treeStyle},
	}

	maxLines := h - 3
	if maxLines < 1 {
		maxLines = 1
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(lines) == maxLines {
			break
		}
		lines = append(lines, contextRow(innerW, row.label, row.value, row.style))
	}

	return renderPanel("Context", strings.Join(lines, "\n"), w, h, false)
}

// contextRow lays out a label on the left and a value flush right within
// innerW cells, clipping both to avoid wrapping.
func contextRow(innerW int, label, value string, valueStyle lipgloss.Style) string {
	label = truncate(label, innerW)
	avail := innerW - lipgloss.Width(label) - 1
	if avail < 1 {
		return valueStyle.Render(truncate(label, innerW))
	}
	value = truncate(value, avail)
	gap := innerW - lipgloss.Width(label) - lipgloss.Width(value)
	if gap < 1 {
		gap = 1
	}
	return label + strings.Repeat(" ", gap) + valueStyle.Render(value)
}
