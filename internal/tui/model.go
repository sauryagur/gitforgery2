package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// maxLogLines bounds the retained harness log so a chatty run cannot grow the
// execution panel's history without limit.
const maxLogLines = 5000

type runResultMsg struct {
	patchDir string
	patches  []string
	// tree is the original HEAD tree captured by startRun. Update persists it
	// onto the Model as originalTree; it cannot be set from the command closure
	// because Update/handleKey have value receivers.
	tree string
	// stats summarizes the rewritten range for the session header; originalDir
	// holds the pre-edit copies of the patches so the review screen can show
	// the original side of the diff.
	stats       git.SessionStats
	originalDir string
	err         error
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

// elapsedMsg re-arms the run timer once per second while the harness runs so
// the execution panel can show a live elapsed time.
type elapsedMsg struct{}

// harnessOutputMsg carries a single line of harness stdout/stderr streamed
// live while a harness command runs. ch is the per-run channel the line came
// from; Update re-arms that channel so a superseded run's late output cannot
// resume draining a newer run's channel.
type harnessOutputMsg struct {
	line  string
	ch    chan tea.Msg
	at    time.Time
	stage string
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

	available []string
	// harnessVersions records each available harness's reported version so the
	// setup list and status bars can name it without re-running --version.
	harnessVersions map[string]string
	setupIdx        int
	rangeInput      textinput.Model
	rangeSpec       string
	rangeBase       string
	// rangeErr holds a range validation failure so the range screen can show
	// it inline and let the user correct the input instead of ending the flow.
	rangeErr string
	// pushRemote/pushBranch name the upstream resolved on the review screen;
	// pushErr records why it could not be resolved so the confirm screen can
	// refuse to push instead of dying mid-rewrite.
	pushRemote string
	pushBranch string
	pushErr    string
	// pushing is set while doPush is in flight so the confirm screen ignores
	// repeated confirmations until the result arrives. pushCancel aborts the
	// in-flight push (bounded by a 3-minute timeout) when the user presses
	// Ctrl+C.
	pushing     bool
	pushCancel  context.CancelFunc
	instrArea   textarea.Model
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

	// step/stepStatus drive the workflow sidebar; step is the current stage and
	// stepStatus records how each stage went so the sidebar can colour them.
	step       int
	stepStatus [stepCount]stepStatus

	// session summarizes the confirmed range for the sidebar, and the run
	// timer plus harness log feed the execution panel.
	session    git.SessionStats
	logs       []logLine
	runStarted time.Time
	elapsed    string

	// originalDir holds the pre-edit patches and showOriginal selects which
	// side of the diff the review screen shows.
	originalDir  string
	showOriginal bool
	helpVisible  bool
	// diffLines is the selected patch's parsed diff, windowed into
	// workspaceView.Diff through diffOffset.
	diffLines  []diffLine
	diffOffset int
	// patchItems caches the reviewed patches' metadata so workspace() does not
	// re-read every patch file on each frame.
	patchItems []patchItem
	// pushed records a successful force-push for the header state chip.
	pushed bool

	spinner   spinner.Model
	runStatus string
	patchDir  string
	patches   []string
	// patchNames holds the Subject: header (or base name) of each patch, used
	// as the short label in the review screen's patch list.
	patchNames []string
	runErr     error
	// runCancel aborts the in-flight run/generation; cancelled marks a run the
	// user stopped so its (error) result is discarded instead of reported.
	runCancel context.CancelFunc
	cancelled bool

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
	ia := textarea.New()
	ia.Placeholder = "rewrite commit messages to be concise"
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	m := Model{
		screen:         scrSetup,
		width:          80,
		height:         24,
		repo:           repo,
		rangeInput:     ti,
		instrArea:      ia,
		textarea:       ta,
		viewport:       viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		outputViewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		spinner:        sp,
	}
	m.stepStatus[stepRange] = stepActive
	m.available = harness.Detect()
	m.harnessVersions = harnessVersions(m.available)
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

// harnessVersions maps each detected harness name to the version it reports,
// so screens can name the active harness without re-running --version.
func harnessVersions(names []string) map[string]string {
	vs := make(map[string]string, len(names))
	for _, name := range names {
		vs[name] = harness.Version(name)
	}
	return vs
}

// prefillRange sets the range input to the repo's default range when the
// input is still empty. When no default range can be derived (for example a
// repository with fewer than two commits) the input is left empty and the
// reason is reported on the range screen.
func (m *Model) prefillRange() {
	if strings.TrimSpace(m.rangeInput.Value()) != "" {
		return
	}
	r, err := git.DefaultRange(m.repo)
	if err != nil {
		m.rangeErr = err.Error()
		return
	}
	m.rangeInput.SetValue(r)
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == "ctrl+c" {
			// A push in flight is abortable: cancel it (the 3-minute timeout
			// bounds it otherwise) and quit rather than waiting it out.
			if m.pushing {
				if m.pushCancel != nil {
					m.pushCancel()
				}
				return m, tea.Quit
			}
			if m.runCancel != nil {
				m.runCancel()
				m.runCancel = nil
			}
			// A run/generate abort leaves a patch directory behind and no
			// screen to clean it up from, so remove it before quitting.
			if m.screen == scrRun || m.screen == scrGenerate {
				m.cleanup()
			}
			return m, tea.Quit
		}
		// Ignore input while a push is in flight so the confirm (or any other
		// queued key) cannot start a second push.
		if m.pushing {
			return m, nil
		}
		// Help is a global overlay: it toggles on every screen, and esc closes
		// it before the active screen (or its focused widget) sees the key.
		if keyMsg.String() == "?" {
			m.helpVisible = !m.helpVisible
			return m, nil
		}
		if keyMsg.String() == "esc" && m.helpVisible {
			m.helpVisible = false
			return m, nil
		}
		// In the review screen's view mode the pager keys scroll the windowed
		// diff; every other key keeps its selection/edit/accept behavior below
		// (and in handleKey).
		if m.screen == scrReview && !m.editing {
			switch keyMsg.String() {
			case "pgup":
				m.scrollDiff(-m.diffViewHeight())
				return m, nil
			case "pgdown":
				m.scrollDiff(m.diffViewHeight())
				return m, nil
			case "ctrl+u":
				m.scrollDiff(-1)
				return m, nil
			case "ctrl+d":
				m.scrollDiff(1)
				return m, nil
			case "home":
				m.diffOffset = 0
				return m, nil
			case "end":
				m.diffOffset = max(0, len(m.diffLines)-m.diffViewHeight())
				return m, nil
			}
		}
		switch {
		case m.screen == scrRange && keyMsg.String() != "enter" && keyMsg.String() != "ctrl+h":
			var cmd tea.Cmd
			m.rangeInput, cmd = m.rangeInput.Update(msg)
			return m, cmd
		case m.screen == scrInstruction && keyMsg.String() != "esc" && keyMsg.String() != "tab" && keyMsg.String() != "ctrl+enter" && keyMsg.String() != "ctrl+s":
			var cmd tea.Cmd
			m.instrArea, cmd = m.instrArea.Update(msg)
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
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	case harnessOutputMsg:
		m.runOutput = append(m.runOutput, msg.line)
		if len(m.runOutput) > maxRunOutputLines {
			m.runOutput = m.runOutput[len(m.runOutput)-maxRunOutputLines:]
		}
		m.outputViewport.SetContent(strings.Join(m.runOutput, "\n"))
		m.outputViewport.GotoBottom()
		at := msg.at
		if at.IsZero() {
			at = time.Now()
		}
		m.logs = append(m.logs, logLine{At: at.Format("15:04:05"), Stage: msg.stage, Text: msg.line})
		if len(m.logs) > maxLogLines {
			m.logs = m.logs[len(m.logs)-maxLogLines:]
		}
		// Keep draining on any screen so background output never blocks the
		// harness process. Re-arm the channel this line came from so output
		// from a superseded run cannot resume draining a newer run's channel.
		ch := msg.ch
		if ch == nil {
			ch = m.runCh
		}
		if ch != nil {
			return m, waitForRun(ch)
		}
		return m, nil
	case runResultMsg:
		if m.cancelled {
			// The user cancelled this run; drop its (cancellation) error and
			// keep the patch directory the aborted run left behind.
			m.cancelled = false
			m.patchDir = msg.patchDir
			return m, nil
		}
		m.runCh = nil
		m.reprompting = false
		m.patchDir = msg.patchDir
		m.patches = msg.patches
		m.runErr = msg.err
		if msg.err != nil {
			m.runStatus = "error: " + msg.err.Error()
			m.finalErr = msg.err
			m.finalMsg = msg.err.Error()
			if m.patchDir != "" {
				m.finalMsg += "\npatch dir: " + m.patchDir
			}
			m.screen = scrDone
			return m, nil
		}
		if msg.tree != "" {
			m.originalTree = msg.tree
		}
		m.runStatus = "done"
		m.runCancel = nil
		m.pushErr = ""
		m.pushRemote = ""
		m.pushBranch = ""
		m.screen = scrReview
		m.selected = 0
		m.patchNames = patchLabels(m.patches)
		m.textarea.Blur()
		// The run finished; the review stage is now active. Validation is
		// satisfied up front when the harness must not modify the final tree.
		m.stepStatus[stepRun] = stepDone
		m.step = stepReview
		m.stepStatus[stepReview] = stepActive
		if m.dontModify {
			m.stepStatus[stepValidate] = stepDone
		} else {
			m.stepStatus[stepValidate] = stepPending
		}
		// A reprompt reuses the original stats and originals; only a fresh
		// run reports them, so never blank the session on an empty result.
		if msg.stats != (git.SessionStats{}) {
			m.session = msg.stats
		}
		if msg.originalDir != "" {
			m.originalDir = msg.originalDir
		}
		m.patchItems = patchItemsFrom(m.patches)
		m.showOriginal = false
		m.diffOffset = 0
		m.diffLines = nil
		if len(m.patches) > 0 {
			m.diffLines = patchDiffLines(m.patches[0])
		}
		m.runStarted = time.Now()
		m.logs = nil
		m.elapsed = ""
		// resize sizes the review panes and refreshes the diff viewport.
		m.resize()
		return m, nil
	case pushResultMsg:
		m.pushing = false
		if m.pushCancel != nil {
			m.pushCancel()
			m.pushCancel = nil
		}
		if msg.err != nil {
			m.finalErr = msg.err
			m.finalMsg = msg.err.Error()
			if m.patchDir != "" {
				m.finalMsg += "\npatch dir: " + m.patchDir
			}
		} else {
			m.finalMsg = msg.msg
			m.pushed = true
			m.stepStatus[stepPush] = stepDone
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
		m.runCancel = nil
		m.screen = scrInstruction
		m.step = stepInstruction
		m.stepStatus[stepInstruction] = stepActive
		if msg.err != nil {
			m.instructionStatus = "generation failed: " + msg.err.Error()
			m.instrArea.SetValue("")
		} else {
			m.instructionStatus = "generated; edit and press ctrl+s"
			if strings.TrimSpace(m.instrArea.Value()) == "" {
				m.instrArea.SetValue(msg.text)
			}
		}
		m.instrArea.Focus()
		return m, nil
	case elapsedMsg:
		if !m.runStarted.IsZero() {
			m.elapsed = time.Since(m.runStarted).Round(time.Second).String()
		}
		if m.screen == scrRun {
			return m, tickElapsed()
		}
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
		m.instrArea, cmd = m.instrArea.Update(msg)
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
					m.reloadDiff()
				}
			case tea.MouseWheelDown:
				if m.selected < len(m.patches)-1 {
					m.selected++
					m.loadSelectedPatch()
					m.reloadDiff()
				}
			}
			return m, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scrollDiff(-1)
		case tea.MouseWheelDown:
			m.scrollDiff(1)
		}
		return m, nil
	case scrRun, scrGenerate:
		var cmd tea.Cmd
		m.outputViewport, cmd = m.outputViewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleClick selects the patch under a left-pane click on the review screen's
// view mode. Every other click is a no-op.
func (m Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.screen != scrReview || m.editing {
		return m, nil
	}
	x0, lw, start, visible := m.reviewListMetrics()
	if msg.X < x0 || msg.X >= x0+lw {
		return m, nil
	}
	// The list content starts two rows down (top border plus title row), and
	// each patch occupies two lines.
	if msg.Y < 2 {
		return m, nil
	}
	idx := start + (msg.Y-2)/2
	if idx < start || idx >= start+visible || idx >= len(m.patches) {
		return m, nil
	}
	m.selected = idx
	m.loadSelectedPatch()
	m.reloadDiff()
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
			m.instrArea.Blur()
			m.textarea.Blur()
		}
	case scrRange:
		if key == "ctrl+h" {
			// Reopen the harness picker, preselecting the active harness and
			// refreshing what is currently installed.
			m.available = harness.Detect()
			m.harnessVersions = harnessVersions(m.available)
			m.setupIdx = 0
			if m.h != nil {
				for i, name := range m.available {
					if name == m.h.Name() {
						m.setupIdx = i
						break
					}
				}
			}
			m.rangeInput.Blur()
			m.instrArea.Blur()
			m.textarea.Blur()
			m.screen = scrSetup
			return m, nil
		}
		if key == "enter" {
			m.rangeErr = ""
			v := strings.TrimSpace(m.rangeInput.Value())
			if v == "" {
				r, err := git.DefaultRange(m.repo)
				if err != nil {
					m.rangeErr = err.Error()
					m.rangeInput.Focus()
					return m, nil
				}
				v = r
			}
			m.rangeSpec = v
			base, end, err := git.ParseRange(m.repo, v)
			if err != nil {
				m.rangeErr = err.Error()
				m.rangeInput.Focus()
				return m, nil
			}
			head, err := git.CurrentTip(m.repo)
			if err != nil {
				m.rangeErr = err.Error()
				m.rangeInput.Focus()
				return m, nil
			}
			if end != head {
				m.rangeErr = fmt.Sprintf("range must end at HEAD: range ends at %s, HEAD is %s", end, head)
				m.rangeInput.Focus()
				return m, nil
			}
			m.rangeBase = base
			if stats, err := git.CollectSessionStats(m.repo, base, end); err == nil {
				m.session = stats
			}
			// A fresh range restarts from the top of the flow; any reprompt
			// intent from a prior review does not carry over.
			m.reprompting = false
			m.rangeInput.Blur()
			m.instructionStatus = "generating instruction..."
			m.stepStatus[stepRange] = stepDone
			m.step = stepInstruction
			for i := stepRun; i < stepCount; i++ {
				m.stepStatus[i] = stepPending
			}
			m.stepStatus[stepInstruction] = stepActive
			m.screen = scrGenerate
			cmd := m.generateInstruction()
			return m, cmd
		}
	case scrGenerate:
		if key == "esc" {
			// Cancel the in-flight generation so its child process dies.
			if m.runCancel != nil {
				m.runCancel()
				m.runCancel = nil
			}
			// Bump the sequence so any in-flight generation result is dropped.
			m.generateSeq++
			m.screen = scrInstruction
			m.step = stepInstruction
			m.stepStatus[stepInstruction] = stepActive
			m.instrArea.Focus()
			m.instructionStatus = "generation skipped"
			// Keep draining the skipped generation's output so its writer
			// never blocks; the stale result leaves the drain alone.
			if m.runCh != nil {
				return m, waitForRun(m.runCh)
			}
		}
	case scrRun:
		if key == "esc" || key == "c" {
			if m.runCancel != nil {
				m.runCancel()
				m.runCancel = nil
			}
			m.cancelled = true
			m.finalErr = fmt.Errorf("cancelled")
			m.finalMsg = "cancelled"
			m.screen = scrDone
		}
	case scrInstruction:
		switch key {
		case "tab":
			m.dontModify = !m.dontModify
		case "esc":
			// Back out to the range screen, keeping the typed instruction so
			// it is still there if the user returns, and drop any reprompt
			// intent so the flow restarts cleanly.
			m.reprompting = false
			m.instrArea.Blur()
			m.rangeInput.Focus()
			m.screen = scrRange
			m.step = stepRange
			m.stepStatus[stepRange] = stepActive
			m.stepStatus[stepInstruction] = stepPending
		case "ctrl+enter", "ctrl+s":
			m.instrArea.Blur()
			m.instructionStatus = ""
			m.instruction = strings.TrimSpace(m.instrArea.Value())
			if m.instruction == "" {
				m.instruction = "improve commit messages"
			}
			m.screen = scrRun
			m.runStatus = "running"
			m.stepStatus[stepInstruction] = stepDone
			m.step = stepRun
			m.stepStatus[stepRun] = stepActive
			for i := stepReview; i < stepCount; i++ {
				m.stepStatus[i] = stepPending
			}
			m.runStarted = time.Now()
			m.elapsed = ""
			m.logs = nil
			if m.reprompting {
				return m, tea.Batch(m.rerunHarness(), tickElapsed())
			}
			return m, tea.Batch(m.startRun(), tickElapsed())
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
				m.reloadDiff()
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
				m.reloadDiff()
			}
		case "down", "j":
			if m.selected < len(m.patches)-1 {
				m.selected++
				m.loadSelectedPatch()
				m.reloadDiff()
			}
		case "e":
			m.editing = true
			m.loadSelectedPatch()
			m.textarea.Focus()
		case "o":
			// Flip between the harness-edited patch and the copy taken before
			// the run; reload so the diff pane follows the toggle.
			m.showOriginal = !m.showOriginal
			m.diffOffset = 0
			m.reloadDiff()
			if m.showOriginal {
				m.reviewStatus = "showing original"
			} else {
				m.reviewStatus = ""
			}
		case "r":
			m.editing = false
			m.textarea.Blur()
			m.reprompting = true
			m.instrArea.SetValue(m.instruction)
			m.screen = scrInstruction
			m.step = stepInstruction
			m.stepStatus[stepInstruction] = stepActive
			m.instrArea.Focus()
		case "a", "enter":
			m.pushErr = ""
			remote, branch, err := git.RemoteBranch(m.repo)
			if err != nil {
				m.pushErr = err.Error()
			} else {
				m.pushRemote = remote
				m.pushBranch = branch
			}
			m.stepStatus[stepValidate] = stepDone
			m.step = stepPush
			m.stepStatus[stepPush] = stepActive
			m.screen = scrConfirm
		case "q", "esc":
			m.cleanup()
			m.finalMsg = "aborted"
			m.screen = scrDone
		}
	case scrConfirm:
		switch key {
		case "y", "enter":
			if m.pushErr != "" || m.pushing {
				return m, nil
			}
			m.pushing = true
			// doPush stores its cancel func on the model, so capture it before
			// returning the updated model.
			cmd := m.doPush()
			return m, cmd
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

// tickElapsed schedules the next elapsed-time tick. Update re-arms it while
// the run screen is active and stops re-arming everywhere else.
func tickElapsed() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return elapsedMsg{} })
}

// diffViewHeight returns how many diff rows the review pane can show. The
// layout reserves one header row, one footer row, and each panel's two border
// rows plus its title row.
func (m Model) diffViewHeight() int {
	_, ch := m.dims()
	h := ch - 5
	if h < 1 {
		h = 1
	}
	return h
}

// scrollDiff moves the diff window by delta rows, clamped to the diff bounds.
func (m *Model) scrollDiff(delta int) {
	maxOffset := len(m.diffLines) - m.diffViewHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	m.diffOffset = clampInt(m.diffOffset+delta, 0, maxOffset)
}

// reloadDiff rebuilds diffLines for the selected patch, honouring the
// showOriginal toggle. Missing originals fall back to the working patch.
func (m *Model) reloadDiff() {
	m.diffLines = nil
	if m.selected < 0 || m.selected >= len(m.patches) {
		return
	}
	path := m.patches[m.selected]
	if m.showOriginal && m.originalDir != "" {
		orig := filepath.Join(m.originalDir, filepath.Base(path))
		if data, err := os.ReadFile(orig); err == nil {
			m.diffLines = parseDiffLines(string(data))
			return
		}
	}
	m.diffLines = patchDiffLines(path)
}

// patchDiffLines parses a patch file into renderable diff lines. An unreadable
// patch yields no lines rather than failing the whole screen.
func patchDiffLines(path string) []diffLine {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseDiffLines(string(data))
}

// workspace assembles the view model the shell and panels render. It is the
// single translation point from Model state to workspaceView, so renderers
// never read Model fields directly.
func (m Model) workspace() workspaceView {
	w, h := m.dims()
	v := workspaceView{Width: w, Height: h}

	v.Branch = m.session.Branch
	v.Pushed = m.pushed

	v.Step = m.step
	v.StepStatus = m.stepStatus

	v.Commits = m.session.Commits
	v.Files = m.session.Files
	v.Additions = m.session.Additions
	v.Deletions = m.session.Deletions

	if m.h != nil {
		name := m.h.Name()
		v.Harness = name
		v.HarnessVersion = m.harnessVersions[name]
	}
	v.HarnessStatus = m.runStatus
	if v.HarnessStatus == "" {
		v.HarnessStatus = m.instructionStatus
	}
	if v.HarnessStatus == "" {
		v.HarnessStatus = "idle"
	}
	v.HarnessRunning = m.runCancel != nil || m.screen == scrRun || m.screen == scrGenerate

	v.RangeDesc = m.rangeSpec
	v.PatchCount = len(m.patches)
	v.FinalTreeMatch = m.dontModify

	v.Logs = m.logs
	v.Elapsed = m.elapsed

	v.Instruction = m.instruction
	switch {
	case m.screen == scrInstruction:
		v.Instruction = m.instrArea.Value()
		v.InstructionLine = m.instrArea.Line()
	case v.Instruction == "":
		v.Instruction = m.instrArea.Value()
	}
	v.InstructionEditing = m.screen == scrInstruction
	v.InstructionErr = m.instructionStatus

	v.Patches = m.patchItems
	if len(v.Patches) == 0 && len(m.patches) > 0 {
		v.Patches = patchItemsFrom(m.patches)
	}
	v.Selected = m.selected
	start, _ := m.reviewListStart()
	v.ListStart = start
	v.Diff, v.DiffTotal = diffWindow(m.diffLines, m.diffOffset, m.diffViewHeight())
	v.DiffOffset = m.diffOffset
	v.ShowOriginal = m.showOriginal
	v.EditingPatch = m.editing
	v.ReviewStatus = m.reviewStatus

	v.HelpVisible = m.helpVisible

	v.Harnesses = m.available
	v.HarnessIdx = m.setupIdx
	v.RangeValue = m.rangeInput.Value()
	v.RangeErr = m.rangeErr

	// The setup, range, confirm and done screens render as modal layers over
	// the workspace rather than as their own full-screen forms.
	switch m.screen {
	case scrSetup:
		v.ModalVisible = true
		v.ModalTitle = "Select Harness"
		v.ModalBody = "Choose the agent harness that will edit the patches."
		v.ModalHint = "↑/↓ choose · enter confirm"
	case scrRange:
		v.ModalVisible = true
		v.ModalTitle = "Select Range"
		v.ModalHint = "enter confirm · ctrl+h change harness"
	case scrConfirm:
		v.ModalVisible = true
		v.ModalTitle = "Confirm force-push"
		var b strings.Builder
		b.WriteString("Force-push the rewritten history?")
		_, _ = fmt.Fprintf(&b, "\n\n%d patch(es) will be applied", len(m.patches))
		switch {
		case m.pushErr != "":
			_, _ = fmt.Fprintf(&b, "\n\ntarget unavailable: %s", m.pushErr)
			v.ModalHint = "n/q abort"
		case m.pushRemote != "" || m.pushBranch != "":
			_, _ = fmt.Fprintf(&b, "\n\ntarget: %s/%s", m.pushRemote, m.pushBranch)
		}
		if m.pushing {
			b.Reset()
			b.WriteString("Pushing...")
			v.ModalHint = "please wait"
		}
		v.ModalBody = b.String()
		if v.ModalHint == "" {
			v.ModalHint = "y/enter push · n abort"
		}
	case scrDone:
		v.ModalVisible = true
		v.ModalTitle = "Done"
		if m.finalErr != nil {
			v.ModalTitle = "Error"
		}
		v.ModalBody = m.finalMsg
		v.ModalHint = "enter/q exit"
	}

	return v
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
