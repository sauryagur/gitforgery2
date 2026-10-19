package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sauryagur/gitforgery2/internal/config"
	"github.com/sauryagur/gitforgery2/internal/gitx"
	"github.com/sauryagur/gitforgery2/internal/harness"
	"github.com/sauryagur/gitforgery2/internal/snapshot"
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
	if r, err := gitx.DefaultRange(m.repo); err == nil {
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
				v, _ = gitx.DefaultRange(m.repo)
			}
			m.rangeSpec = v
			b, err := gitx.RangeBase(m.repo, v)
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

// maxRunOutputLines bounds the retained harness output so a chatty command
// cannot grow runOutput without limit.
const maxRunOutputLines = 5000

// streamCommand runs bin/args in dir, streams every stdout/stderr line to ch
// as harnessOutputMsg, and returns the full combined output.
func streamCommand(ctx context.Context, bin string, args []string, dir string, ch chan<- tea.Msg) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	var lines []string
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			lines = append(lines, line)
			if ch != nil {
				ch <- harnessOutputMsg{line: line}
			}
		}
		if sc.Err() != nil {
			// A single line exceeded the buffer: keep reading (and dropping)
			// so the child's writer never blocks.
			_, _ = io.Copy(io.Discard, pr)
		}
	}()

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		<-scanned
		return "", err
	}
	// The scanner consumes the pipe while the command runs, so the writer
	// never blocks; Wait then returns once both process and output are done.
	waitErr := cmd.Wait()
	_ = pw.Close()
	<-scanned
	return strings.Join(lines, "\n"), waitErr
}

// waitForRun returns a command that delivers the next message from ch, or nil
// once the channel is closed.
func waitForRun(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// generateInstruction feeds the current git log for the confirmed range to the
// selected harness and asks it for a plain-text rewrite instruction.
func (m *Model) generateInstruction() tea.Cmd {
	m.generateSeq++
	seq := m.generateSeq
	mc := *m
	ch := make(chan tea.Msg, 256)
	m.runCh = ch
	m.runOutput = nil
	m.outputViewport.SetContent("")
	return func() tea.Msg {
		go func() {
			defer close(ch)
			ch <- generateInstructionResult(mc, seq, ch)
		}()
		return waitForRun(ch)()
	}
}

// generateInstructionResult does the work of generateInstruction in the
// background; m is a snapshot so the goroutine never reads the live Model.
func generateInstructionResult(m Model, seq int, ch chan tea.Msg) instructionResultMsg {
	repo := m.repo
	rangeSpec := m.rangeSpec
	h := m.h
	if h == nil {
		return instructionResultMsg{seq: seq, err: fmt.Errorf("no harness selected")}
	}
	log, err := gitx.Log(repo, rangeSpec)
	if err != nil {
		return instructionResultMsg{seq: seq, err: err}
	}
	if strings.TrimSpace(log) == "" {
		return instructionResultMsg{seq: seq, err: fmt.Errorf("no commits in range %s", rangeSpec)}
	}
	prompt, err := harness.InstructionPrompt(log)
	if err != nil {
		return instructionResultMsg{seq: seq, err: err}
	}
	bin, args := h.Command(prompt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := streamCommand(ctx, bin, args, repo, ch)
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return instructionResultMsg{seq: seq, err: fmt.Errorf("harness failed: %w\n%s", err, string(ee.Stderr))}
		}
		return instructionResultMsg{seq: seq, err: fmt.Errorf("harness failed: %w", err)}
	}
	text := strings.TrimSpace(out)
	if text == "" {
		return instructionResultMsg{seq: seq, err: fmt.Errorf("harness produced an empty instruction")}
	}
	return instructionResultMsg{seq: seq, text: text}
}

func (m *Model) startRun() tea.Cmd {
	mc := *m
	ch := make(chan tea.Msg, 256)
	m.runCh = ch
	m.runOutput = nil
	m.outputViewport.SetContent("")
	return func() tea.Msg {
		go func() {
			defer close(ch)
			ch <- startRunResult(mc, ch)
		}()
		return waitForRun(ch)()
	}
}

// startRunResult backs up the current history, materialises the patches and
// runs the harness in the patch directory; m is a snapshot of the Model.
func startRunResult(m Model, ch chan tea.Msg) runResultMsg {
	repo := m.repo
	if err := gitx.CreateBackupRef(repo); err != nil {
		return runResultMsg{err: err}
	}
	tip, err := gitx.CurrentTip(repo)
	if err != nil {
		return runResultMsg{err: err}
	}
	tree, err := gitx.TreeOf(repo, "HEAD")
	if err != nil {
		return runResultMsg{err: err}
	}
	if err := snapshot.Save(repo, snapshot.Snapshot{Tip: tip, Tree: tree}); err != nil {
		return runResultMsg{err: err}
	}

	tmp, err := os.MkdirTemp("", "gitforgery2-patches-*")
	if err != nil {
		return runResultMsg{err: err}
	}
	if err := gitx.FormatPatch(repo, m.rangeSpec, tmp); err != nil {
		return runResultMsg{err: err}
	}
	patches, err := gitx.PatchFiles(tmp)
	if err != nil {
		return runResultMsg{err: err}
	}
	m.patches = patches
	bin, args := m.h.Command(editPrompt(m.instruction))
	out, err := streamCommand(context.Background(), bin, args, tmp, ch)
	if err != nil {
		return runResultMsg{patchDir: tmp, patches: patches, err: fmt.Errorf("harness failed: %w\n%s", err, out)}
	}
	if m.dontModify {
		if err := checkFinalState(m, tree); err != nil {
			return runResultMsg{patchDir: tmp, patches: patches, err: err}
		}
	}
	return runResultMsg{patchDir: tmp, patches: patches, tree: tree}
}

// rerunHarness re-runs the harness against the existing patch directory after
// the user edits the instruction. Unlike startRun it must not create a backup
// ref, save a snapshot, or re-run format-patch (which would discard edits).
func (m *Model) rerunHarness() tea.Cmd {
	mc := *m
	ch := make(chan tea.Msg, 256)
	m.runCh = ch
	m.runOutput = nil
	m.outputViewport.SetContent("")
	return func() tea.Msg {
		go func() {
			defer close(ch)
			ch <- rerunHarnessResult(mc, ch)
		}()
		return waitForRun(ch)()
	}
}

// rerunHarnessResult re-runs the harness over the existing patch directory;
// m is a snapshot of the Model.
func rerunHarnessResult(m Model, ch chan tea.Msg) runResultMsg {
	if m.patchDir == "" || len(m.patches) == 0 {
		return runResultMsg{err: fmt.Errorf("nothing to reprompt: no patches")}
	}
	bin, args := m.h.Command(editPrompt(m.instruction))
	out, err := streamCommand(context.Background(), bin, args, m.patchDir, ch)
	if err != nil {
		return runResultMsg{patchDir: m.patchDir, patches: m.patches, err: fmt.Errorf("harness failed: %w\n%s", err, out)}
	}
	if m.dontModify {
		if err := checkFinalState(m, m.originalTree); err != nil {
			return runResultMsg{patchDir: m.patchDir, patches: m.patches, err: err}
		}
	}
	return runResultMsg{patchDir: m.patchDir, patches: m.patches}
}

// checkFinalState verifies that applying all patches to m.rangeBase yields
// wantTree (RFC §5). It fails closed: any error computing the resulting tree
// is returned, and a mismatch reports a diff summary.
func checkFinalState(m Model, wantTree string) error {
	got, err := gitx.ResultingTree(m.repo, m.rangeBase, m.patches)
	if err != nil {
		return fmt.Errorf("final state check failed: %w", err)
	}
	if got != wantTree {
		summary, derr := gitx.DiffStat(m.repo, wantTree, got)
		if derr != nil || strings.TrimSpace(summary) == "" {
			return fmt.Errorf("final state modified:\ntree %s != %s", got, wantTree)
		}
		return fmt.Errorf("final state modified:\n%s", summary)
	}
	return nil
}

// checkFinalState is the Model method form used by the flow; it delegates to
// the package-level helper so the streaming goroutines can call it with a
// Model snapshot.
func (m Model) checkFinalState(wantTree string) error {
	return checkFinalState(m, wantTree)
}

// editPrompt is the harness prompt used by both the initial run and reprompts.
func editPrompt(instruction string) string {
	return fmt.Sprintf("You are editing git format-patch files in the current directory. Edit them in place according to this instruction: %s. Only modify patch contents; do not create or delete files.", instruction)
}

func (m *Model) doPush() tea.Cmd {
	return func() tea.Msg {
		repo := m.repo
		patches := m.patches
		if m.dontModify {
			snap, err := snapshot.Load(repo)
			if err != nil {
				return pushResultMsg{err: fmt.Errorf("final state check failed: %w", err)}
			}
			if err := m.checkFinalState(snap.Tree); err != nil {
				return pushResultMsg{err: err}
			}
		}
		if m.rangeBase == "" {
			return pushResultMsg{err: fmt.Errorf("range base is empty; cannot reset before applying patches")}
		}
		if _, err := gitx.Run(repo, "reset", "--hard", m.rangeBase); err != nil {
			return pushResultMsg{err: err}
		}
		if err := gitx.Am(repo, patches); err != nil {
			_, _ = gitx.Run(repo, "am", "--abort")
			_, _ = gitx.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
			m.cleanup()
			return pushResultMsg{err: err}
		}
		if m.dontModify {
			snap, err := snapshot.Load(repo)
			if err != nil {
				_, _ = gitx.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
				return pushResultMsg{err: fmt.Errorf("final state check failed: %w", err)}
			}
			newTree, err := gitx.TreeOf(repo, "HEAD")
			if err != nil {
				_, _ = gitx.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
				return pushResultMsg{err: err}
			}
			if newTree != snap.Tree {
				_, _ = gitx.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
				return pushResultMsg{err: fmt.Errorf("final state modified:\ntree %s != %s", newTree, snap.Tree)}
			}
		}
		remote, branch, err := gitx.RemoteBranch(repo)
		if err != nil {
			m.cleanup()
			return pushResultMsg{err: err}
		}
		if err := gitx.ForcePush(repo, remote, branch); err != nil {
			m.cleanup()
			return pushResultMsg{err: err}
		}
		m.cleanup()
		return pushResultMsg{msg: "pushed " + remote + "/" + branch}
	}
}

func (m *Model) cleanup() {
	if m.patchDir != "" {
		_ = os.RemoveAll(m.patchDir)
		m.patchDir = ""
	}
}

func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
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
		return m.renderForm("Commit range", m.rangeInput.View(), "Enter continue")
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
		return m.renderForm("Running", m.outputViewport.View(), m.spinner.View()+" "+m.runStatus)
	case scrReview:
		return m.renderReview()
	case scrConfirm:
		var b strings.Builder
		b.WriteString("Force-push the rewritten history?")
		_, _ = fmt.Fprintf(&b, "\n\n%d patch(es) will be applied", len(m.patches))
		return m.renderForm("Confirm force-push", b.String(), "y/n")
	case scrDone:
		if m.finalErr != nil {
			return m.renderForm("Error", errorStyle.Render(m.finalErr.Error()), "Enter/q exit")
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
