package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// renderExecution draws the harness execution panel: the active harness and
// its status, the tail of the harness log, and the elapsed time. It occupies
// exactly w×h cells. renderPanel draws the border and title, so the body gets
// w-2 columns and h-3 rows (two border rows, one title row).
func renderExecution(v workspaceView, w, h int) string {
	innerW := w - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := h - 3
	if innerH < 1 {
		innerH = 1
	}

	lines := make([]string, 0, innerH)
	lines = append(lines, renderExecutionHeader(v, innerW))

	// Reserve one row each for the header and the elapsed footer, leaving
	// h-5 rows for the log itself.
	maxRows := h - 5
	if maxRows < 1 {
		maxRows = 1
	}
	logs := v.Logs
	if len(logs) > maxRows {
		logs = logs[len(logs)-maxRows:]
	}

	if len(logs) == 0 {
		lines = append(lines, mutedStyle.Render(truncate("Waiting for harness...", innerW)))
	} else {
		lines = append(lines, renderLogLines(logs, innerW)...)
	}

	if v.Elapsed != "" {
		lines = append(lines, mutedStyle.Render(truncate("elapsed "+v.Elapsed, innerW)))
	}

	// Defensive clip: a tiny panel must never overflow its border.
	if len(lines) > innerH {
		lines = lines[:innerH]
	}

	return renderPanel("Harness Execution", strings.Join(lines, "\n"), w, h, v.HarnessRunning)
}

// renderExecutionHeader renders the "harness · status" subtitle with a
// right-aligned cancel hint while the harness is running. The hint sits on the
// same row as the subtitle so the panel stays dense.
func renderExecutionHeader(v workspaceView, width int) string {
	parts := make([]string, 0, 2)
	if v.Harness != "" {
		parts = append(parts, v.Harness)
	}
	if v.HarnessStatus != "" {
		parts = append(parts, v.HarnessStatus)
	}
	left := ""
	if len(parts) > 0 {
		left = mutedStyle.Render(truncate(strings.Join(parts, " · "), width))
	}
	if !v.HarnessRunning {
		return left
	}

	hint := keycapStyle.Render("[c]") + " " + mutedStyle.Render("cancel")
	hintW := lipgloss.Width(hint)
	leftW := lipgloss.Width(left)
	gap := width - leftW - hintW
	if gap < 1 {
		// Too tight for both; the actionable hint wins.
		if hintW <= width {
			return strings.Repeat(" ", width-hintW) + hint
		}
		return left
	}
	return left + strings.Repeat(" ", gap) + hint
}

// renderLogLines formats log entries as aligned "At  Stage  Text" rows, with
// the timestamp muted, the stage accented and empty fields omitted. Each row is
// clipped to width; text is truncated before styling because truncate is not
// ANSI-aware.
func renderLogLines(logs []logLine, width int) []string {
	atW, stageW := 0, 0
	for _, l := range logs {
		if n := lipgloss.Width(l.At); n > atW {
			atW = n
		}
		if n := lipgloss.Width(l.Stage); n > stageW {
			stageW = n
		}
	}

	rows := make([]string, 0, len(logs))
	for _, l := range logs {
		var b strings.Builder
		prefixW := 0

		if l.At != "" {
			b.WriteString(mutedStyle.Render(l.At))
			if pad := atW - lipgloss.Width(l.At); pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
			b.WriteString("  ")
			prefixW += atW + 2
		}
		if l.Stage != "" {
			stage := l.Stage
			room := width - prefixW - stageW - 2
			if room < lipgloss.Width(stage) {
				if room <= 0 {
					stage = ""
				} else {
					stage = truncate(stage, room)
				}
			}
			if stage != "" {
				b.WriteString(accentStyle.Render(stage))
				if pad := stageW - lipgloss.Width(stage); pad > 0 {
					b.WriteString(strings.Repeat(" ", pad))
				}
				b.WriteString("  ")
				prefixW += stageW + 2
			}
		}

		text := l.Text
		if text != "" {
			if room := width - prefixW; room > 0 {
				text = truncate(text, room)
			} else {
				text = ""
			}
			b.WriteString(text)
		}

		rows = append(rows, b.String())
	}
	return rows
}
