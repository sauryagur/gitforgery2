package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/sauryagur/gitforgery2/internal/config"
	"github.com/sauryagur/gitforgery2/internal/gitx"
	"github.com/sauryagur/gitforgery2/internal/harness"
	"github.com/sauryagur/gitforgery2/internal/snapshot"
)

const (
	scrSetup = iota
	scrRange
	scrInstruction
	scrRun
	scrReview
	scrConfirm
	scrDone
)

type runResultMsg struct {
	patchDir string
	patches  []string
	err      error
}

type pushResultMsg struct {
	err error
	msg string
}

type Model struct {
	screen int

	repo string
	cfg  *config.Config
	h    harness.Harness

	available []string
	setupIdx  int
	rangeInput textinput.Model
	rangeSpec  string
	rangeBase  string
	instrInput  textinput.Model
	instruction string
	dontModify  bool

	spinner   spinner.Model
	runStatus string
	patchDir  string
	patches   []string
	runErr    error

	selected     int
	textarea     textarea.Model
	editing      bool
	reviewStatus string

	confirmMsg string

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
		screen:      scrSetup,
		repo:        repo,
		rangeInput:  ti,
		instrInput:  textinput.New(),
		textarea:    ta,
		spinner:     sp,
	}
	m.instrInput.Placeholder = "rewrite commit messages to be concise"
	m.available = harness.Detect()

	// Reuse persisted selection when it is still available.
	if cfg, err := config.Load(); err == nil && cfg.Harness != "" {
		for _, name := range m.available {
			if name == cfg.Harness {
				h, err := harness.New(cfg.Harness)
				if err == nil {
					m.h = h
					m.cfg = cfg
					m.screen = scrRange
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

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		nm, cmd := m.handleKey(msg.String())
		return nm, cmd
	case runResultMsg:
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
		m.runStatus = "done"
		m.screen = scrReview
		m.selected = 0
		m.loadSelectedPatch()
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
		if key == "up" || key == "k" {
			if m.setupIdx > 0 {
				m.setupIdx--
			}
		} else if key == "down" || key == "j" {
			if m.setupIdx < len(m.available)-1 {
				m.setupIdx++
			}
		} else if key == "enter" {
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
			m.rangeInput.Focus()
		}
	case scrRange:
		if key == "enter" {
			v := strings.TrimSpace(m.rangeInput.Value())
			if v == "" {
				v, _ = gitx.DefaultRange(m.repo)
			}
			m.rangeSpec = v
			if b, err := gitx.RangeBase(m.repo, v); err == nil {
				m.rangeBase = b
			}
			m.screen = scrInstruction
			m.instrInput.Focus()
		}
	case scrInstruction:
		if key == "tab" {
			m.dontModify = !m.dontModify
		} else if key == "enter" {
			m.instruction = strings.TrimSpace(m.instrInput.Value())
			if m.instruction == "" {
				m.instruction = "improve commit messages"
			}
			m.screen = scrRun
			return m, m.startRun()
		}
	case scrReview:
		if m.editing {
			if key == "ctrl+s" {
				if m.selected < len(m.patches) {
					_ = os.WriteFile(m.patches[m.selected], []byte(m.textarea.Value()), 0o644)
				}
				m.editing = false
				m.reviewStatus = "saved"
			} else if key == "esc" {
				m.editing = false
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
		case "r":
			m.screen = scrRun
			return m, m.startRun()
		case "a", "enter":
			m.screen = scrConfirm
		case "q", "esc":
			m.cleanup()
			m.finalMsg = "aborted"
			m.screen = scrDone
		}
	case scrConfirm:
		if key == "y" || key == "enter" {
			return m, m.doPush()
		} else if key == "n" || key == "q" || key == "esc" {
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

func (m *Model) loadSelectedPatch() {
	if m.selected < 0 || m.selected >= len(m.patches) {
		return
	}
	b, err := os.ReadFile(m.patches[m.selected])
	if err != nil {
		m.textarea.SetValue(err.Error())
		return
	}
	m.textarea.SetValue(string(b))
}

func (m *Model) startRun() tea.Cmd {
	return func() tea.Msg {
		repo := m.repo
		_ = gitx.CreateBackupRef(repo)
		tip, _ := gitx.CurrentTip(repo)
		tree, _ := gitx.TreeOf(repo, "HEAD")
		_ = snapshot.Save(repo, snapshot.Snapshot{Tip: tip, Tree: tree})

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
		prompt := fmt.Sprintf("You are editing git format-patch files in the current directory. Edit them in place according to this instruction: %s. Only modify patch contents; do not create or delete files.", m.instruction)
		bin, args := m.h.Command(prompt)
		cmd := exec.Command(bin, args...)
		cmd.Dir = tmp
		out, err := cmd.CombinedOutput()
		if err != nil {
			return runResultMsg{patchDir: tmp, patches: patches, err: fmt.Errorf("harness failed: %w\n%s", err, string(out))}
		}
		if m.dontModify {
			if err := gitx.ApplyCheck(repo, patches); err != nil {
				return runResultMsg{patchDir: tmp, patches: patches, err: fmt.Errorf("patch lint failed: %w", err)}
			}
			resultTree, err := gitx.ResultingTree(repo, m.rangeBase, patches)
			if err != nil {
				return runResultMsg{patchDir: tmp, patches: patches, err: fmt.Errorf("final state check failed: %w", err)}
			}
			if resultTree != tree {
				return runResultMsg{patchDir: tmp, patches: patches, err: fmt.Errorf("final state modified: tree %s != %s", resultTree, tree)}
			}
		}
		return runResultMsg{patchDir: tmp, patches: patches, err: nil}
	}
}

func (m *Model) doPush() tea.Cmd {
	return func() tea.Msg {
		repo := m.repo
		patches := m.patches
		if m.dontModify {
			if err := gitx.ApplyCheck(repo, patches); err != nil {
				return pushResultMsg{err: fmt.Errorf("patch lint failed: %w", err)}
			}
			resultTree, err := gitx.ResultingTree(repo, m.rangeBase, patches)
			if err != nil {
				return pushResultMsg{err: fmt.Errorf("final state check failed: %w", err)}
			}
			snap, err := snapshot.Load(repo)
			if err == nil && resultTree != snap.Tree {
				return pushResultMsg{err: fmt.Errorf("final state modified: tree %s != %s", resultTree, snap.Tree)}
		}
		}
		if m.rangeBase != "" {
			if _, err := gitx.Run(repo, "reset", "--hard", m.rangeBase); err != nil {
				return pushResultMsg{err: err}
			}
		}
		if err := gitx.Am(repo, patches); err != nil {
			m.cleanup()
			return pushResultMsg{err: err}
		}
		if m.dontModify {
			snap, err := snapshot.Load(repo)
			if err == nil {
				newTree, _ := gitx.TreeOf(repo, "HEAD")
				if newTree != snap.Tree {
					_, _ = gitx.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
					m.cleanup()
					return pushResultMsg{err: fmt.Errorf("final state modified: tree %s != %s", newTree, snap.Tree)}
				}
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
		b.WriteString("Setup: select harness\n\n")
		if len(m.available) == 0 {
			b.WriteString("No harness found (pi, codex, claude). Install one and re-run.\n")
		} else {
			for i, h := range m.available {
				cursor := "  "
				if i == m.setupIdx {
					cursor = "> "
				}
				b.WriteString(cursor + h + "\n")
			}
		}
		b.WriteString("\nEnter select, Ctrl+C quit\n")
		return b.String()
	case scrRange:
		return "Range (git rev-list):\n" + m.rangeInput.View() + "\n\nEnter to continue"
	case scrInstruction:
		return "Instruction for harness:\n" + m.instrInput.View() +
			"\n\nTab: toggle 'Don't modify final state' = " + boolStr(m.dontModify) +
			"\nEnter to run"
	case scrRun:
		return m.spinner.View() + " " + m.runStatus + "\n"
	case scrReview:
		var b strings.Builder
		b.WriteString("Patches:\n")
		for i, p := range m.patches {
			cursor := "  "
			if i == m.selected {
				cursor = "> "
			}
			b.WriteString(cursor + filepath.Base(p) + "\n")
		}
		b.WriteString("\n")
		if m.editing {
			b.WriteString(m.textarea.View() + "\n")
			b.WriteString("Ctrl+S save, Esc cancel\n")
		} else {
			b.WriteString("e edit, r reprompt, a accept, q abort\n")
			if m.reviewStatus != "" {
				b.WriteString(m.reviewStatus + "\n")
			}
		}
		return b.String()
	case scrConfirm:
		return "Confirm force-push? (y/n)\n" + m.confirmMsg
	case scrDone:
		if m.finalErr != nil {
			return "Error: " + m.finalErr.Error() + "\n\nEnter/q to exit"
		}
		return "Done: " + m.finalMsg + "\n\nEnter/q to exit"
	}
	return ""
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
