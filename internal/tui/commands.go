package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sauryagur/gitforgery2/internal/git"
	"github.com/sauryagur/gitforgery2/internal/harness"
	"github.com/sauryagur/gitforgery2/internal/snapshot"
)

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
	log, err := git.Log(repo, rangeSpec)
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
	if err := git.CreateBackupRef(repo); err != nil {
		return runResultMsg{err: err}
	}
	tip, err := git.CurrentTip(repo)
	if err != nil {
		return runResultMsg{err: err}
	}
	tree, err := git.TreeOf(repo, "HEAD")
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
	if err := git.FormatPatch(repo, m.rangeSpec, tmp); err != nil {
		return runResultMsg{err: err}
	}
	patches, err := git.PatchFiles(tmp)
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
	got, err := git.ResultingTree(m.repo, m.rangeBase, m.patches)
	if err != nil {
		return fmt.Errorf("final state check failed: %w", err)
	}
	if got != wantTree {
		summary, derr := git.DiffStat(m.repo, wantTree, got)
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
		if _, err := git.Run(repo, "reset", "--hard", m.rangeBase); err != nil {
			return pushResultMsg{err: err}
		}
		if err := git.Am(repo, patches); err != nil {
			_, _ = git.Run(repo, "am", "--abort")
			_, _ = git.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
			m.cleanup()
			return pushResultMsg{err: err}
		}
		if m.dontModify {
			snap, err := snapshot.Load(repo)
			if err != nil {
				_, _ = git.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
				return pushResultMsg{err: fmt.Errorf("final state check failed: %w", err)}
			}
			newTree, err := git.TreeOf(repo, "HEAD")
			if err != nil {
				_, _ = git.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
				return pushResultMsg{err: err}
			}
			if newTree != snap.Tree {
				_, _ = git.Run(repo, "reset", "--hard", "refs/gitforgery2/original")
				return pushResultMsg{err: fmt.Errorf("final state modified:\ntree %s != %s", newTree, snap.Tree)}
			}
		}
		remote, branch, err := git.RemoteBranch(repo)
		if err != nil {
			m.cleanup()
			return pushResultMsg{err: err}
		}
		if err := git.ForcePush(repo, remote, branch); err != nil {
			m.cleanup()
			return pushResultMsg{err: err}
		}
		m.cleanup()
		return pushResultMsg{msg: "pushed " + remote + "/" + branch}
	}
}
