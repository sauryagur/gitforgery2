package tui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/sauryagur/gitforgery2/internal/config"
	"github.com/sauryagur/gitforgery2/internal/git"
	"github.com/sauryagur/gitforgery2/internal/harness"
)

const (
	scrSetup = iota
	scrRange
	scrGenerate
	scrInstruction
	scrRun
	scrReview
	scrConfirm
	scrDone
)

type runResultMsg struct {
	patchDir string
	patches  []string
	// tree is the original HEAD tree captured by startRun. Update persists it
	// onto the Model as originalTree; it cannot be set from the command closure
	// because Update/handleKey have value receivers.
	tree string
	err  error
}

type pushResultMsg struct {
	err error
	msg string
}

type instructionResultMsg struct {
	seq  int
	text string
	err  error
}

// harnessOutputMsg carries a single line of harness stdout/stderr streamed
// live while a harness command runs.
type harnessOutputMsg struct {
	line string
}

type Model struct {
	screen int

	// width/height are the terminal dimensions, kept up to date from
	// tea.WindowSizeMsg; they default to 80x24 when unset.
	width  int
	height int

	repo string
	cfg  *config.Config
	h    harness.Harness

	available   []string
	setupIdx    int
	rangeInput  textinput.Model
	rangeSpec   string
	rangeBase   string
	instrInput  textinput.Model
	instruction string
	// instructionStatus is shown while generating and on the instruction
	// screen; generateSeq tags generation results so stale ones are dropped.
	instructionStatus string
	generateSeq       int
	dontModify        bool
	reprompting       bool
	// originalTree is the HEAD tree at the start of the run, reused when
	// reprompting so the final-state check still compares against it.
	originalTree string

	spinner   spinner.Model
	runStatus string
	patchDir  string
	patches   []string
	// patchNames holds the Subject: header (or base name) of each patch, used
	// as the short label in the review screen's patch list.
	patchNames []string
	runErr     error

	// runCh streams harness stdout/stderr lines while a command runs;
	// runOutput accumulates them for outputViewport, which shows the live
	// output on the run and generate screens.
	runCh          chan tea.Msg
	runOutput      []string
	outputViewport viewport.Model

	selected     int
	textarea     textarea.Model
	viewport     viewport.Model
	editing      bool
	reviewStatus string

	finalMsg string
	finalErr error
}

func New(repo string) Model {
	ti := textinput.New()
	ti.Placeholder = "HEAD~5..HEAD"
	ti.Focus()
	ta := textarea.New()
	ta.Placeholder = "patch contents"
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	m := Model{
		screen:         scrSetup,
		width:          80,
		height:         24,
		repo:           repo,
		rangeInput:     ti,
		instrInput:     textinput.New(),
		textarea:       ta,
		viewport:       viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		outputViewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		spinner:        sp,
	}
	m.instrInput.Placeholder = "rewrite commit messages to be concise"
	m.available = harness.Detect()
	m.resize()

	// Reuse persisted selection when it is still available.
	if cfg, err := config.Load(); err == nil && cfg.Harness != "" {
		for _, name := range m.available {
			if name == cfg.Harness {
				h, err := harness.New(cfg.Harness)
				if err == nil {
					m.h = h
					m.cfg = cfg
					m.screen = scrRange
					m.prefillRange()
					m.rangeInput.Focus()
					return m
				}
			}
		}
	}

	// Auto-select when exactly one harness is available.
	if len(m.available) == 1 {
		h, err := harness.New(m.available[0])
		if err == nil {
			m.h = h
			cfg := &config.Config{Harness: h.Name(), Version: harness.Version(h.Name())}
			_ = config.Save(cfg)
			m.cfg = cfg
			m.screen = scrRange
			m.prefillRange()
			m.rangeInput.Focus()
			return m
		}
	}

	if len(m.available) == 0 {
		m.finalErr = fmt.Errorf("no harness found; install pi, codex, or claude")
		m.finalMsg = m.finalErr.Error()
		m.screen = scrDone
	}
	return m
}

// prefillRange sets the range input to the repo's default range when the
// input is still empty (errors are ignored).
func (m *Model) prefillRange() {
	if strings.TrimSpace(m.rangeInput.Value()) != "" {
		return
	}
	if r, err := git.DefaultRange(m.repo); err == nil {
		m.rangeInput.SetValue(r)
	}
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// In the review screen's view mode the pager keys scroll the diff
		// viewport; every other key keeps its selection/edit/accept behavior
		// below (and in handleKey).
		if m.screen == scrReview && !m.editing {
			switch keyMsg.String() {
			case "pgup", "pgdown", "ctrl+u", "ctrl+d":
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			case "home":
				m.viewport.GotoTop()
				return m, nil
			case "end":
				m.viewport.GotoBottom()
				return m, nil
			}
		}
		switch {
		case m.screen == scrRange && keyMsg.String() != "enter":
			var cmd tea.Cmd
			m.rangeInput, cmd = m.rangeInput.Update(msg)
			return m, cmd
		case m.screen == scrInstruction && keyMsg.String() != "enter" && keyMsg.String() != "tab":
			var cmd tea.Cmd
			m.instrInput, cmd = m.instrInput.Update(msg)
			return m, cmd
		case m.screen == scrReview && m.editing && keyMsg.String() != "ctrl+s" && keyMsg.String() != "esc":
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}
		nm, cmd := m.handleKey(keyMsg.String())
		return nm, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case tea.MouseWheelMsg:
		return m.handleWheel(msg)
	case harnessOutputMsg:
		m.runOutput = append(m.runOutput, msg.line)
		if len(m.runOutput) > maxRunOutputLines {
			m.runOutput = m.runOutput[len(m.runOutput)-maxRunOutputLines:]
		}
		m.outputViewport.SetContent(strings.Join(m.runOutput, "\n"))
		m.outputViewport.GotoBottom()
		// Keep draining on any screen so background output never blocks the
		// harness process.
		if m.runCh != nil {
			return m, waitForRun(m.runCh)
		}
		return m, nil
	case runResultMsg:
		m.runCh = nil
		m.reprompting = false
		m.patchDir = msg.patchDir
		m.patches = msg.patches
		m.runErr = msg.err
		if msg.err != nil {
			m.runStatus = "error: " + msg.err.Error()
			m.finalErr = msg.err
			m.finalMsg = msg.err.Error()
			m.screen = scrDone
			return m, nil
		}
		if msg.tree != "" {
			m.originalTree = msg.tree
		}
		m.runStatus = "done"
		m.screen = scrReview
		m.selected = 0
		m.patchNames = patchLabels(m.patches)
		m.textarea.Blur()
		// resize sizes the review panes and refreshes the diff viewport.
		m.resize()
		return m, nil
	case pushResultMsg:
		if msg.err != nil {
			m.finalErr = msg.err
			m.finalMsg = msg.err.Error()
		} else {
			m.finalMsg = msg.msg
		}
		m.screen = scrDone
		return m, nil
	case instructionResultMsg:
		if msg.seq != m.generateSeq {
			// Superseded generation: its output is already stale, and runCh
			// may now belong to a newer run, so leave the drain alone.
			return m, nil
		}
		m.runCh = nil
		m.screen = scrInstruction
		if msg.err != nil {
			m.instructionStatus = "generation failed: " + msg.err.Error()
			m.instrInput.SetValue("")
		} else {
			m.instructionStatus = "generated; edit and press enter"
			if strings.TrimSpace(m.instrInput.Value()) == "" {
				m.instrInput.SetValue(msg.text)
			}
		}
		m.instrInput.Focus()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	if m.screen == scrRange {
		var cmd tea.Cmd
		m.rangeInput, cmd = m.rangeInput.Update(msg)
		return m, cmd
	}
	if m.screen == scrInstruction {
		var cmd tea.Cmd
		m.instrInput, cmd = m.instrInput.Update(msg)
		return m, cmd
	}
	if m.screen == scrReview && m.editing {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleWheel scrolls the pane under the cursor: the patch list on the left of
// the review screen, the diff on the right, or the run/generate output.
func (m Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case scrReview:
		if m.editing {
			return m, nil
		}
		lw, _ := m.reviewPaneWidths()
		if msg.X < lw {
			switch msg.Button {
			case tea.MouseWheelUp:
				if m.selected > 0 {
					m.selected--
					m.loadSelectedPatch()
				}
			case tea.MouseWheelDown:
				if m.selected < len(m.patches)-1 {
					m.selected++
					m.loadSelectedPatch()
				}
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case scrRun, scrGenerate:
		var cmd tea.Cmd
		m.outputViewport, cmd = m.outputViewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(key string) (Model, tea.Cmd) {
	switch m.screen {
	case scrSetup:
		switch key {
		case "up", "k":
			if m.setupIdx > 0 {
				m.setupIdx--
			}
		case "down", "j":
			if m.setupIdx < len(m.available)-1 {
				m.setupIdx++
			}
		case "enter":
			if len(m.available) == 0 {
				m.finalErr = fmt.Errorf("no harness found; install pi, codex, or claude")
				m.finalMsg = m.finalErr.Error()
				m.screen = scrDone
				return m, nil
			}
			h, err := harness.New(m.available[m.setupIdx])
			if err != nil {
				m.finalErr = err
				m.finalMsg = err.Error()
				m.screen = scrDone
				return m, nil
			}
			m.h = h
			cfg := &config.Config{Harness: h.Name(), Version: harness.Version(h.Name())}
			if err := config.Save(cfg); err != nil {
				m.finalErr = err
				m.finalMsg = err.Error()
				m.screen = scrDone
				return m, nil
			}
			m.cfg = cfg
			m.screen = scrRange
			m.prefillRange()
			m.rangeInput.Focus()
			m.instrInput.Blur()
			m.textarea.Blur()
		}
	case scrRange:
		if key == "enter" {
			v := strings.TrimSpace(m.rangeInput.Value())
			if v == "" {
				v, _ = git.DefaultRange(m.repo)
			}
			m.rangeSpec = v
			b, err := git.RangeBase(m.repo, v)
			if err != nil {
				m.finalErr = err
				m.finalMsg = err.Error()
				m.screen = scrDone
				return m, nil
			}
			m.rangeBase = b
			m.rangeInput.Blur()
			m.instructionStatus = "generating instruction..."
			m.screen = scrGenerate
			cmd := m.generateInstruction()
			return m, cmd
		}
	case scrGenerate:
		if key == "esc" {
			// Bump the sequence so any in-flight generation result is dropped.
			m.generateSeq++
			m.screen = scrInstruction
			m.instrInput.Focus()
			m.instructionStatus = "generation skipped"
			// Keep draining the skipped generation's output so its writer
			// never blocks; the stale result leaves the drain alone.
			if m.runCh != nil {
				return m, waitForRun(m.runCh)
			}
		}
	case scrInstruction:
		switch key {
		case "tab":
			m.dontModify = !m.dontModify
		case "enter":
			m.instrInput.Blur()
			m.instructionStatus = ""
			m.instruction = strings.TrimSpace(m.instrInput.Value())
			if m.instruction == "" {
				m.instruction = "improve commit messages"
			}
			m.screen = scrRun
			if m.reprompting {
				cmd := m.rerunHarness()
				return m, cmd
			}
			cmd := m.startRun()
			return m, cmd
		}
	case scrReview:
		if m.editing {
			switch key {
			case "ctrl+s":
				if m.selected >= len(m.patches) {
					return m, nil
				}
				err := os.WriteFile(m.patches[m.selected], []byte(m.textarea.Value()), 0o644)
				if err != nil {
					m.reviewStatus = "save failed: " + err.Error()
					return m, nil
				}
				m.editing = false
				m.textarea.Blur()
				m.reviewStatus = "saved"
				m.refreshPatchView()
			case "esc":
				m.editing = false
				m.textarea.Blur()
			}
			return m, nil
		}
		switch key {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.loadSelectedPatch()
			}
		case "down", "j":
			if m.selected < len(m.patches)-1 {
				m.selected++
				m.loadSelectedPatch()
			}
		case "e":
			m.editing = true
			m.loadSelectedPatch()
			m.textarea.Focus()
		case "r":
			m.editing = false
			m.textarea.Blur()
			m.reprompting = true
			m.instrInput.SetValue(m.instruction)
			m.screen = scrInstruction
			m.instrInput.Focus()
		case "a", "enter":
			m.screen = scrConfirm
		case "q", "esc":
			m.cleanup()
			m.finalMsg = "aborted"
			m.screen = scrDone
		}
	case scrConfirm:
		switch key {
		case "y", "enter":
			return m, m.doPush()
		case "n", "q", "esc":
			m.cleanup()
			m.finalMsg = "aborted before push"
			m.screen = scrDone
		}
	case scrDone:
		if key == "enter" || key == "q" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) cleanup() {
	if m.patchDir != "" {
		_ = os.RemoveAll(m.patchDir)
		m.patchDir = ""
	}
}

func Run(repo string) error {
	m := New(repo)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
