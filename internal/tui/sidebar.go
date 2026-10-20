package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderSidebar draws the workflow sidebar: the six-step workflow, session
// stats, and harness status. It renders a single column and clips content to
// the w×h pane; the view model owns all state, this file only lays it out.
func renderSidebar(v workspaceView, w, h int) string {
	// Match renderPanel's minimums so our content width never exceeds the
	// frame's inner width (styled lines truncated mid-escape would break).
	if w < 4 {
		w = 4
	}
	if h < 3 {
		h = 3
	}
	inner := w - 2

	steps := make([]string, 0, stepCount)
	for i := range stepCount {
		steps = append(steps, sidebarStepLine(i, v.StepStatus[i], inner))
	}

	session := []string{
		sidebarHeader("Session Info", inner),
		sidebarBranch(v.Branch, inner),
		sidebarCount(v.Commits, "commits selected", inner),
		sidebarCount(v.Files, "files changed", inner),
		sidebarChanges(v.Additions, v.Deletions, inner),
	}

	harness := []string{
		sidebarHeader("Harness", inner),
		sidebarHarnessName(v.Harness, v.HarnessVersion, inner),
		sidebarHarnessStatus(v.HarnessRunning, inner),
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinVertical(lipgloss.Left, steps...),
		sidebarRule(inner),
		lipgloss.JoinVertical(lipgloss.Left, session...),
		sidebarRule(inner),
		lipgloss.JoinVertical(lipgloss.Left, harness...),
	)

	return renderPanel("Workflow", body, w, h, true)
}

// sidebarStepLine renders one workflow row: a number, a status icon, and the
// step title. Done rows are green, the active row is highlighted full-width,
// and pending rows are muted.
func sidebarStepLine(i int, status stepStatus, width int) string {
	num := strconv.Itoa(i + 1)
	title := stepTitles[i]

	switch status {
	case stepDone:
		const reserved = 2 // " " + "✓ "
		budget := width - lipgloss.Width(num) - reserved
		if budget > 0 {
			return mutedStyle.Render(num+" ") +
				successStyle.Render("✓ "+truncate(title, budget))
		}
		return successStyle.Render(truncate(num+" ✓ "+title, width))
	case stepActive:
		plain := truncate(num+" ▶ "+title, width)
		if pad := width - lipgloss.Width(plain); pad > 0 {
			plain += strings.Repeat(" ", pad)
		}
		return selectedRowStyle.Render(plain)
	default:
		return mutedStyle.Render(truncate(num+" ○ "+title, width))
	}
}

// sidebarHeader renders a section heading.
func sidebarHeader(label string, width int) string {
	return headerStyle.Render(truncate(label, width))
}

// sidebarRule renders a dim horizontal separator spanning the column.
func sidebarRule(width int) string {
	if width < 1 {
		width = 1
	}
	return mutedStyle.Render(strings.Repeat("─", width))
}

// sidebarBranch renders the current branch, or a muted placeholder when the
// session is not attached to one.
func sidebarBranch(branch string, width int) string {
	const prefix = "⎇ "
	if branch == "" {
		return mutedStyle.Render(truncate("branch: —", width))
	}
	budget := width - lipgloss.Width(prefix)
	if budget < 1 {
		return mutedStyle.Render(truncate(prefix+branch, width))
	}
	return mutedStyle.Render(prefix) + accentStyle.Render(truncate(branch, budget))
}

// sidebarCount renders a counted session stat, e.g. "3 commits selected".
func sidebarCount(n int, label string, width int) string {
	num := strconv.Itoa(n)
	suffix := " " + label
	if lipgloss.Width(num)+lipgloss.Width(suffix) <= width {
		return accentStyle.Render(num) + mutedStyle.Render(suffix)
	}
	return mutedStyle.Render(truncate(num+suffix, width))
}

// sidebarChanges renders the aggregated diff line counts: additions in green,
// deletions in red.
func sidebarChanges(additions, deletions, width int) string {
	add := "+" + strconv.Itoa(additions)
	del := "−" + strconv.Itoa(deletions)
	plain := add + " " + del
	if lipgloss.Width(plain) <= width {
		return successStyle.Render(add) + " " + dangerStyle.Render(del)
	}
	return mutedStyle.Render(truncate(plain, width))
}

// sidebarHarnessName renders "name version", with the version dimmed.
func sidebarHarnessName(name, version string, width int) string {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" {
		return mutedStyle.Render(truncate("—", width))
	}
	budget := width - lipgloss.Width(name) - 1
	if version == "" || budget < 1 {
		return accentStyle.Render(truncate(name, width))
	}
	ver := truncate(version, budget)
	if ver == "" {
		return accentStyle.Render(truncate(name, width))
	}
	return accentStyle.Render(name) + mutedStyle.Render(" "+ver)
}

// sidebarHarnessStatus shows a green live dot while the harness runs and a
// quiet "idle" otherwise.
func sidebarHarnessStatus(running bool, width int) string {
	if running {
		return successStyle.Render(truncate("● Running...", width))
	}
	return mutedStyle.Render(truncate("idle", width))
}
