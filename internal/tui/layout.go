package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sauryagur/gitforgery2/internal/git"
)

// loadSelectedPatch reads the selected patch into both the textarea (edit
// mode) and the diff viewport (view mode). Out-of-range selections clear both.
func (m *Model) loadSelectedPatch() {
	m.reloadDiff()
	m.diffOffset = 0
	if m.selected < 0 || m.selected >= len(m.patches) {
		m.textarea.SetValue("")
		m.viewport.SetContent("")
		return
	}
	b, err := os.ReadFile(m.patches[m.selected])
	if err != nil {
		m.textarea.SetValue(err.Error())
		m.viewport.SetContent(err.Error())
		return
	}
	text := string(b)
	m.textarea.SetValue(text)
	m.viewport.SetContent(text)
	m.viewport.SetYOffset(0)
}

// refreshPatchView reloads the selected patch so the diff pane matches the
// current patch list and pane size. The scroll offset survives a resize;
// diffWindow clamps it to the new content.
func (m *Model) refreshPatchView() {
	offset := m.diffOffset
	m.loadSelectedPatch()
	m.diffOffset = offset
}

// resize recomputes widget sizes from the terminal dimensions. Zero
// dimensions fall back to 80x24.
func (m *Model) resize() {
	if m.width <= 0 {
		m.width = 80
	}
	if m.height <= 0 {
		m.height = 24
	}
	inputW := m.formBoxWidth() - 2
	if inputW < 10 {
		inputW = 10
	}
	m.rangeInput.SetWidth(inputW)
	m.instrArea.SetWidth(inputW)
	m.instrArea.SetHeight(6)

	_, ch := m.dims()
	bodyH := ch - 1 // bottom status bar
	if bodyH < 3 {
		bodyH = 3
	}
	_, rw := m.reviewPaneWidths()
	innerW := rw - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := bodyH - 3 // borders + title
	if innerH < 1 {
		innerH = 1
	}
	m.viewport.SetWidth(innerW)
	m.viewport.SetHeight(innerH)
	m.textarea.SetWidth(innerW)
	m.textarea.SetHeight(innerH)

	ow := m.formBoxWidth() - 2
	if ow < 1 {
		ow = 1
	}
	oh := m.height - 6
	if oh < 3 {
		oh = 3
	}
	m.outputViewport.SetWidth(ow)
	m.outputViewport.SetHeight(oh)
	// While the user is editing a patch a resize must only reflow the widgets;
	// reloading the patch would discard the unsaved edits.
	if !m.editing {
		m.refreshPatchView()
	}
}

// dims returns the terminal size, falling back to 80x24 when unset so that
// rendering never depends on a WindowSizeMsg having arrived.
func (m Model) dims() (int, int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

// formBoxWidth returns the outer width of the centred boxes used by the
// non-review screens.
func (m Model) formBoxWidth() int {
	w, _ := m.dims()
	bw := clampInt(w-8, 30, 72)
	if bw > w {
		bw = w
	}
	if bw < 4 {
		bw = 4
	}
	return bw
}

// reviewPaneWidths splits the terminal width into the patch-list pane (left)
// and the diff pane (right). The widths always sum to w.
func (m Model) reviewPaneWidths() (int, int) {
	w, _ := m.dims()
	lw := clampInt(w/3, 24, 40)
	if lw > w-12 {
		lw = w - 12
	}
	if lw < 10 {
		lw = 10
	}
	rw := w - lw
	if rw < 12 {
		rw = 12
		lw = w - rw
		if lw < 4 {
			lw = 4
			rw = w - lw
		}
	}
	return lw, rw
}

// reviewListMetrics returns the absolute X offset of the patch list, its pane
// width, the first visible patch index, and how many two-line items fit.
func (m Model) reviewListMetrics() (x0, lw, start, visible int) {
	w, h := m.dims()
	sidebarW := 0
	if w >= 100 {
		sidebarW = clampInt(w/5, 24, 30)
	}
	mainW := w - sidebarW
	lw = clampInt(mainW/3, 28, 38)

	_, _, reviewH, _ := splitBodyHeights(max(3, h-2))
	visible = (reviewH - 3) / 2 // borders + title; two-line rows
	if visible < 1 {
		visible = 1
	}

	start = 0
	if m.selected >= visible {
		start = clampInt(m.selected-visible+1, 0, max(0, len(m.patches)-visible))
	}
	x0 = sidebarW
	return x0, lw, start, visible
}

// reviewListStart reports the first visible patch index and how many
// two-line items fit in the review list.
func (m Model) reviewListStart() (start, visible int) {
	_, _, start, visible = m.reviewListMetrics()
	return start, visible
}

// clampInt returns v limited to [low, high].
func clampInt(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// patchLabels maps patch file paths to short labels for the review list.
func patchLabels(paths []string) []string {
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = patchSubject(p)
	}
	return names
}

// patchSubject returns a short label for a format-patch file: the Subject
// header with a leading "[PATCH] " / "[PATCH n/m] " prefix stripped, falling
// back to the file's base name when the header is missing or unreadable.
func patchSubject(path string) string {
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				break // headers end at the first blank line
			}
			if strings.HasPrefix(line, "Subject:") {
				subject := strings.TrimSpace(strings.TrimPrefix(line, "Subject:"))
				if stripped := stripPatchPrefix(subject); stripped != "" {
					return stripped
				}
				break
			}
		}
	}
	return filepath.Base(path)
}

// stripPatchPrefix removes git's bracketed patch prefix ("[PATCH]",
// "[PATCH 2/3]", "[PATCH v2 1/3]", …) from a Subject header value.
func stripPatchPrefix(subject string) string {
	if strings.HasPrefix(subject, "[PATCH") {
		if i := strings.Index(subject, "]"); i >= 0 {
			return strings.TrimSpace(subject[i+1:])
		}
	}
	return subject
}

// parseDiffLines splits unified diff text into classified lines for the diff
// pane. Hunk and file-header lines are tagged so the renderer can style them
// separately; content lines keep their leading '+'/'-' marker.
func parseDiffLines(text string) []diffLine {
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	raw := strings.Split(text, "\n")
	lines := make([]diffLine, 0, len(raw))
	for _, line := range raw {
		var kind diffKind
		switch {
		case strings.HasPrefix(line, "diff --git "),
			strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "---"),
			strings.HasPrefix(line, "+++"):
			kind = diffHeader
		case strings.HasPrefix(line, "@@"):
			kind = diffHunk
		case strings.HasPrefix(line, "+"):
			kind = diffAdd
		case strings.HasPrefix(line, "-"):
			kind = diffDel
		default:
			kind = diffContext
		}
		lines = append(lines, diffLine{Kind: kind, Text: line})
	}
	return lines
}

// patchItemsFrom builds the review-side patch list, hiding git's patch
// machinery behind a short subject and per-patch line counts. Unreadable
// patches yield zero counts rather than an error.
func patchItemsFrom(paths []string) []patchItem {
	items := make([]patchItem, len(paths))
	for i, path := range paths {
		item := patchItem{Index: i + 1, Title: patchSubject(path)}
		if stat, err := git.PatchStats(path); err == nil {
			item.Files = stat.Files
			item.Additions = stat.Additions
			item.Deletions = stat.Deletions
		}
		items[i] = item
	}
	return items
}

// diffWindow returns the lines visible at the given scroll offset and the
// total line count. The offset is clamped so the window never runs past the
// end of the diff.
func diffWindow(lines []diffLine, offset, height int) (visible []diffLine, total int) {
	total = len(lines)
	if height <= 0 {
		return nil, total
	}
	offset = clampInt(offset, 0, max(0, total-height))
	end := offset + height
	if end > total {
		end = total
	}
	return lines[offset:end], total
}
