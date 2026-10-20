package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// viewContent composes the persistent workspace shell: header, optional
// sidebar, the main body (execution, instruction/context and review panels)
// and footer. Help and modals replace the main body while the chrome and
// sidebar stay visible.
func (m Model) viewContent() string {
	v := m.workspace()
	if v.Width <= 0 {
		v.Width = 80
	}
	if v.Height <= 0 {
		v.Height = 24
	}

	header := renderHeader(v)
	footer := renderFooter(v)

	mainH := v.Height - 2
	if mainH < 3 {
		mainH = 3
	}

	sidebarW := 0
	if v.Width >= 100 {
		sidebarW = clampInt(v.Width/5, 24, 30)
	}
	mainW := v.Width - sidebarW
	if mainW < 1 {
		mainW = 1
	}

	body := renderBody(v, mainW, mainH)

	main := body
	if sidebarW > 0 {
		sidebar := renderSidebar(v, sidebarW, mainH)
		main = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, body)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, main, footer)
}

// renderBody lays out the main column at the given size, or the active
// overlay when help or a modal is open.
func renderBody(v workspaceView, w, h int) string {
	if v.HelpVisible {
		return renderHelp(v, w, h)
	}
	if v.ModalVisible {
		return renderModal(v, w, h)
	}

	execH, middleH, reviewH, showMiddle := splitBodyHeights(h)
	parts := make([]string, 0, 3)

	execution := renderExecution(v, w, execH)
	parts = append(parts, execution)

	if showMiddle {
		iw, cw := splitMiddleWidths(w)
		instruction := renderInstruction(v, iw, middleH)
		if cw > 0 {
			parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Top,
				instruction, renderContext(v, cw, middleH)))
		} else {
			parts = append(parts, instruction)
		}
	}

	parts = append(parts, renderReviewPanel(v, w, reviewH))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// splitBodyHeights divides the main column height between the execution
// panel, the instruction/context row and the review panel. The middle row is
// dropped when the remaining space would leave the review panel too short; in
// that case execution and review share the height instead.
func splitBodyHeights(mainH int) (execH, middleH, reviewH int, showMiddle bool) {
	execH = clampInt(mainH/4, 6, 12)
	middleH = clampInt(mainH/3, 8, 14)
	reviewH = mainH - execH - middleH
	if reviewH >= 6 {
		return execH, middleH, reviewH, true
	}

	reviewH = mainH - execH
	if reviewH < 6 {
		// Too small for both minimums: keep the review's six rows and give
		// execution whatever remains, without overrunning the column.
		if execH = mainH - 6; execH < 1 {
			execH = 1
		}
		reviewH = mainH - execH
		if reviewH < 1 {
			reviewH = 1
		}
	}
	return execH, 0, reviewH, false
}

// splitMiddleWidths divides the main column width between the instruction
// panel (about sixty percent) and the context panel. The context panel only
// appears when the column is wide enough for both; otherwise cw is zero and
// the instruction panel takes the full width.
func splitMiddleWidths(mainW int) (iw, cw int) {
	if mainW < 110 {
		return mainW, 0
	}
	iw = mainW * 3 / 5
	if iw < 1 {
		iw = 1
	}
	cw = mainW - iw
	if cw < 1 {
		cw = 0
		iw = mainW
	}
	return iw, cw
}
