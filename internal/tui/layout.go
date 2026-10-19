package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// loadSelectedPatch reads the selected patch into both the textarea (edit
// mode) and the diff viewport (view mode). Out-of-range selections clear both.
func (m *Model) loadSelectedPatch() {
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
// current patch list and pane size.
func (m *Model) refreshPatchView() {
	m.loadSelectedPatch()
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
	m.instrInput.SetWidth(inputW)

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
	m.refreshPatchView()
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
