package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderReviewPanel draws the patch list and diff viewport side by side. The
// caller supplies the full review area dimensions; the panels fill it exactly.
// The left panel owns the indexed patch metadata, the right panel the windowed
// diff plus a scrollbar when the diff is longer than the viewport.
func renderReviewPanel(v workspaceView, w, h int) string {
	if w < 1 {
		w = 1
	}
	if h < 3 {
		h = 3
	}
	// renderPanel enforces a four-cell minimum; below that two panels cannot
	// share the area meaningfully, so show the list alone.
	if w < 8 {
		return renderPanel("Patches", reviewListBody(v, w, h), w, h, true)
	}

	listW := reviewClamp(w/3, 28, 38)
	if listW > w-1 {
		listW = w - 1
	}
	if listW < 1 {
		listW = 1
	}
	diffW := w - listW
	if diffW < 1 {
		diffW = 1
		listW = w - diffW
	}

	left := renderPanel("Patches", reviewListBody(v, listW, h), listW, h, !v.EditingPatch)
	right := renderPanel(reviewDiffTitle(v, diffW), reviewDiffBody(v, diffW, h), diffW, h, v.EditingPatch)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// reviewListBody renders one patch per two lines: its index and title, then a
// metadata line. Rows scroll from v.ListStart and are clipped to the panel.
// reviewStatus, when set, is pinned to the bottom line of the list.
func reviewListBody(v workspaceView, w, h int) string {
	innerW := w - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := h - 2 // border rows
	rowsH := innerH - 1
	if rowsH < 1 {
		rowsH = 1
	}

	status := ""
	if v.ReviewStatus != "" {
		status = truncate(v.ReviewStatus, innerW)
	}
	listH := rowsH
	if status != "" && listH > 1 {
		listH--
	}

	var lines []string
	start := reviewClamp(v.ListStart, 0, max(0, len(v.Patches)-1))
	for i := start; i < len(v.Patches) && len(lines) < listH; i++ {
		p := v.Patches[i]
		idx := p.Index
		if idx <= 0 {
			idx = i + 1
		}
		title := truncate(fmt.Sprintf("%02d · %s", idx, p.Title), innerW)
		meta := truncate(reviewPatchMeta(p), innerW)

		if i == v.Selected {
			lines = append(lines, selectedRowStyle.Width(innerW).Render(title))
			if len(lines) < listH {
				lines = append(lines, selectedRowStyle.Width(innerW).Render(meta))
			}
			continue
		}
		lines = append(lines, sidebarStyle.Render(title))
		if len(lines) < listH {
			lines = append(lines, reviewPatchMetaStyle(p, innerW))
		}
	}

	if len(lines) == 0 {
		lines = append(lines, mutedStyle.Render(truncate("No patches", innerW)))
	}
	if status != "" && len(lines) < rowsH {
		for len(lines) < rowsH-1 {
			lines = append(lines, "")
		}
		lines = append(lines, footerStyle.Render(status))
	}
	return strings.Join(lines, "\n")
}

// reviewPatchMeta returns the metadata line for a patch: the change size when
// additions and deletions differ, otherwise the file count.
func reviewPatchMeta(p patchItem) string {
	if p.Additions != p.Deletions {
		return fmt.Sprintf("+%d −%d", p.Additions, p.Deletions)
	}
	return fmt.Sprintf("Modified · %d files", p.Files)
}

// reviewPatchMetaStyle styles the metadata line, colouring additions green and
// deletions red. File counts stay muted. The line is clipped to w after
// styling so a highlighted row never overflows its panel.
func reviewPatchMetaStyle(p patchItem, w int) string {
	if p.Additions == p.Deletions {
		return mutedStyle.Render(reviewPatchMeta(p))
	}
	add := successStyle.Render(fmt.Sprintf("+%d", p.Additions))
	del := dangerStyle.Render(fmt.Sprintf("−%d", p.Deletions))
	line := add + " " + del
	if gap := w - lipgloss.Width(line); gap < 0 {
		return mutedStyle.Render(truncate(reviewPatchMeta(p), w))
	}
	return line
}

// reviewDiffTitle builds the right panel title with a right-aligned badge
// showing the patch count or an "Original" marker. The returned string is
// plain text so renderPanel can clip it safely.
func reviewDiffTitle(v workspaceView, w int) string {
	innerW := w - 2
	if innerW < 1 {
		innerW = 1
	}
	title := "Patch Review"
	if v.EditingPatch {
		title = "Edit patch"
	}
	badge := "Original"
	if !v.ShowOriginal {
		n := v.PatchCount
		if n <= 0 {
			n = len(v.Patches)
		}
		badge = fmt.Sprintf("%d patches", n)
	}
	titleW := lipgloss.Width(title)
	badgeW := lipgloss.Width(badge)
	if titleW+badgeW+1 <= innerW {
		return title + strings.Repeat(" ", innerW-titleW-badgeW) + badge
	}
	// Narrow panel: shrink the title so the badge stays visible.
	title = truncate(title, innerW-badgeW-1)
	if lipgloss.Width(title) < 1 {
		return truncate(badge, innerW)
	}
	gap := innerW - lipgloss.Width(title) - badgeW
	return title + strings.Repeat(" ", max(1, gap)) + badge
}

// reviewDiffBody renders the windowed diff with per-kind colours and a
// one-column scrollbar pinned to the right edge when the diff is taller than
// the viewport.
func reviewDiffBody(v workspaceView, w, h int) string {
	innerW := w - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := h - 2 // border rows
	visible := innerH - 1
	if visible < 1 {
		visible = 1
	}

	total := v.DiffTotal
	if total <= 0 {
		total = len(v.Diff)
	}
	showScroll := total > visible && innerW >= 2
	textW := innerW
	thumb := make([]bool, visible)
	track := make([]bool, visible)
	if showScroll {
		textW--
		if textW < 1 {
			textW = 1
		}
		thumbH := visible * visible / total
		if thumbH < 1 {
			thumbH = 1
		}
		if thumbH > visible {
			thumbH = visible
		}
		maxStart := visible - thumbH
		maxOffset := total - visible
		offset := reviewClamp(v.DiffOffset, 0, max(0, maxOffset))
		start := 0
		if maxStart > 0 && maxOffset > 0 {
			start = offset * maxStart / maxOffset
		}
		for i := range visible {
			track[i] = true
			if i >= start && i < start+thumbH {
				thumb[i] = true
			}
		}
	}

	lines := make([]string, 0, visible)
	for i := range visible {
		text := ""
		kind := diffContext
		if i < len(v.Diff) {
			text = v.Diff[i].Text
			kind = v.Diff[i].Kind
		}
		styled := reviewDiffStyle(kind).Render(truncate(text, textW))
		if showScroll {
			if gap := textW - lipgloss.Width(styled); gap > 0 {
				styled += strings.Repeat(" ", gap)
			}
			switch {
			case thumb[i]:
				styled += accentStyle.Render("█")
			case track[i]:
				styled += gutterStyle.Render("░")
			default:
				styled += " "
			}
		}
		lines = append(lines, styled)
	}
	return strings.Join(lines, "\n")
}

// reviewDiffStyle returns the style for a diff line kind.
func reviewDiffStyle(kind diffKind) lipgloss.Style {
	switch kind {
	case diffAdd:
		return successStyle
	case diffDel:
		return dangerStyle
	case diffHunk:
		return hunkStyle
	case diffHeader:
		return accentStyle
	default:
		return mutedStyle
	}
}

// reviewClamp bounds v to [lo, hi].
func reviewClamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
