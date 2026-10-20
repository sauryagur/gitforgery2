package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/sauryagur/gitforgery2/internal/harness"
)

// flowRepo describes a temp repo with a bare origin and two rewritable commits.
type flowRepo struct {
	dir    string
	remote string
	base   string
	tip    string
	tree   string
}

func flowGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func flowSetupRepo(t *testing.T) flowRepo {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(t.TempDir(), "origin.git")
	flowGit(t, "", "init", "-q", "--bare", remote)
	flowGit(t, dir, "init", "-q", "-b", "main")
	flowGit(t, dir, "config", "user.email", "a@b.com")
	flowGit(t, dir, "config", "user.name", "A")
	flowGit(t, dir, "remote", "add", "origin", remote)
	for i := 1; i <= 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(fmt.Sprintf("line %d\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		flowGit(t, dir, "add", "f.txt")
		flowGit(t, dir, "commit", "-q", "-m", fmt.Sprintf("commit %d", i))
	}
	flowGit(t, dir, "push", "-q", "-u", "origin", "main")
	return flowRepo{
		dir:    dir,
		remote: remote,
		base:   flowGit(t, dir, "rev-parse", "HEAD~2^{commit}"),
		tip:    flowGit(t, dir, "rev-parse", "HEAD"),
		tree:   flowGit(t, dir, "rev-parse", "HEAD^{tree}"),
	}
}

// flowFakeHarness installs a fake `pi` on PATH. Instruction-generation prompts
// get a canned instruction; edit prompts run editCmd inside the patch dir.
func flowFakeHarness(t *testing.T, editCmd string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do case \"$a\" in --version) echo \"fake-pi 1.0\"; exit 0;; esac; done\n" +
		"case \"$*\" in\n" +
		"  *\"Git log:\"*) echo \"Rewrite commit subjects.\"; exit 0;;\n" +
		"esac\n" + editCmd + "\n"
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func flowModel(repo, base string) Model {
	m := Model{
		screen:      scrInstruction,
		width:       120,
		height:      40,
		repo:        repo,
		h:           harness.Pi{},
		rangeSpec:   "HEAD~2..HEAD",
		rangeBase:   base,
		instruction: "rewrite history",
		dontModify:  true,
		rangeInput:  textinput.New(),
		instrArea:   textarea.New(),
		textarea:    textarea.New(),
		viewport:    viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		spinner:     spinner.New(),
	}
	m.instrArea.SetValue(m.instruction)
	m.resize()
	return m
}

func flowKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Text: string(r), Code: r} }

// flowCtrlEnter is the instruction screen's confirm key; plain enter inserts a
// newline in the multi-line instruction textarea.
func flowCtrlEnter() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
}

// flowStartRun confirms the instruction with ctrl+enter and verifies the flow
// moved to the run screen.
func flowStartRun(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	m, cmd := flowPress(t, m, flowCtrlEnter())
	if m.screen != scrRun {
		t.Fatalf("ctrl+enter on the instruction screen: screen = %d, want scrRun", m.screen)
	}
	return m, cmd
}

func flowPress(t *testing.T, m Model, key tea.KeyPressMsg) (Model, tea.Cmd) {
	t.Helper()
	te, cmd := m.Update(key)
	got, ok := te.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", te)
	}
	return got, cmd
}

// flowDrive executes commands and feeds messages back until done reports true
// or commands stop. It tolerates the harness output stream, which delivers many
// harnessOutputMsg values before the final result message.
func flowDrive(t *testing.T, m Model, cmd tea.Cmd, done func(Model) bool) Model {
	t.Helper()
	for steps := 0; cmd != nil && !done(m); steps++ {
		if steps > 200000 {
			t.Fatal("flowDrive did not terminate")
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = flowDrive(t, m, c, done)
				if done(m) {
					return m
				}
			}
			return m
		}
		if msg == nil {
			break
		}
		te, next := m.Update(msg)
		m = te.(Model)
		cmd = next
	}
	return m
}

// TestFlowRewritesSubjectsAndPushes drives the whole non-interactive flow with
// a fake harness and verifies patch fidelity plus force-push of the rewrite.
func TestFlowRewritesSubjectsAndPushes(t *testing.T) {
	fr := flowSetupRepo(t)
	flowFakeHarness(t, "sed -i 's/^Subject: .*/Subject: rewritten/' ./*.patch")

	m := flowModel(fr.dir, fr.base)
	m, cmd := flowStartRun(t, m)
	m = flowDrive(t, m, cmd, func(m Model) bool { return m.screen == scrReview || m.screen == scrDone })
	if m.finalErr != nil {
		t.Fatalf("run failed: %v", m.finalErr)
	}
	if m.screen != scrReview {
		t.Fatalf("screen = %d, want review", m.screen)
	}

	m, _ = flowPress(t, m, flowKey('a'))
	if m.screen != scrConfirm {
		t.Fatalf("screen = %d, want confirm", m.screen)
	}
	m, cmd = flowPress(t, m, flowKey('y'))
	m = flowDrive(t, m, cmd, func(m Model) bool { return m.screen == scrDone })
	if m.finalErr != nil {
		t.Fatalf("push failed: %v", m.finalErr)
	}
	if !strings.Contains(m.finalMsg, "pushed") {
		t.Fatalf("finalMsg = %q, want a pushed message", m.finalMsg)
	}

	if got := flowGit(t, fr.dir, "rev-parse", "HEAD^{tree}"); got != fr.tree {
		t.Fatalf("local tree %s != %s", got, fr.tree)
	}
	if got := flowGit(t, fr.remote, "rev-parse", "refs/heads/main^{tree}"); got != fr.tree {
		t.Fatalf("remote tree %s != %s", got, fr.tree)
	}
	if log := flowGit(t, fr.remote, "log", "--format=%s", "main"); !strings.Contains(log, "rewritten") {
		t.Fatalf("remote subjects were not rewritten:\n%s", log)
	}
	if got := flowGit(t, fr.dir, "rev-parse", "refs/gitforgery2/original"); got != fr.tip {
		t.Fatalf("backup ref %s != %s", got, fr.tip)
	}
}

// TestFlowFinalStateEnforced verifies that a harness edit which changes the
// resulting tree aborts before commit and leaves the repo untouched.
func TestFlowFinalStateEnforced(t *testing.T) {
	fr := flowSetupRepo(t)
	flowFakeHarness(t, "sed -i 's/^+line 3$/+line 3 CHANGED/' ./*.patch")

	m := flowModel(fr.dir, fr.base)
	m, cmd := flowStartRun(t, m)
	m = flowDrive(t, m, cmd, func(m Model) bool { return m.screen == scrDone })
	if m.finalErr == nil {
		t.Fatal("expected a final-state error")
	}
	if !strings.Contains(m.finalErr.Error(), "final state modified") {
		t.Fatalf("error %q does not mention final state modified", m.finalErr)
	}
	if m.patchDir == "" {
		t.Fatal("temp patch dir was not kept on mismatch")
	}
	if got := flowGit(t, fr.dir, "rev-parse", "HEAD"); got != fr.tip {
		t.Fatalf("HEAD moved on abort: %s != %s", got, fr.tip)
	}
}

// TestStartRunRejectsNonHEADRange verifies the range is validated against HEAD
// before any backup ref, snapshot, or patch directory is created.
func TestStartRunRejectsNonHEADRange(t *testing.T) {
	fr := flowSetupRepo(t)

	m := flowModel(fr.dir, "")
	m.rangeSpec = "HEAD~2..HEAD~1"
	msg := startRunResult(m, context.Background(), nil)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "range must end at HEAD") {
		t.Fatalf("err = %v, want range must end at HEAD", msg.err)
	}
	if msg.patchDir != "" {
		t.Fatalf("patchDir = %q, want empty", msg.patchDir)
	}
	if refExists(t, fr.dir, "refs/gitforgery2/original") {
		t.Fatal("backup ref was created before range validation")
	}
}

// TestStartRunRejectsDirtyWorktree verifies uncommitted tracked changes abort
// the run before anything is written.
func TestStartRunRejectsDirtyWorktree(t *testing.T) {
	fr := flowSetupRepo(t)
	if err := os.WriteFile(filepath.Join(fr.dir, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := flowModel(fr.dir, "")
	msg := startRunResult(m, context.Background(), nil)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "uncommitted changes") {
		t.Fatalf("err = %v, want uncommitted changes", msg.err)
	}
	if msg.patchDir != "" {
		t.Fatalf("patchDir = %q, want empty", msg.patchDir)
	}
	if refExists(t, fr.dir, "refs/gitforgery2/original") {
		t.Fatal("backup ref was created for a dirty worktree")
	}
	if got := flowGit(t, fr.dir, "rev-parse", "HEAD"); got != fr.tip {
		t.Fatalf("HEAD moved on dirty-worktree abort: %s != %s", got, fr.tip)
	}
}

// refExists reports whether ref resolves in repo.
func refExists(t *testing.T, repo, ref string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref)
	cmd.Dir = repo
	return cmd.Run() == nil
}
