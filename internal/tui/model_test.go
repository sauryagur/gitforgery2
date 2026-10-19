package tui

import (
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

	"github.com/sauryagur/gitforgery2/internal/git"
)

// testModel builds a Model with real widgets but without touching config or
// harness detection, so rendering can be exercised in isolation.
func testModel() Model {
	return Model{
		rangeInput:     textinput.New(),
		instrInput:     textinput.New(),
		textarea:       textarea.New(),
		viewport:       viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		outputViewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		spinner:        spinner.New(),
	}
}

// realPatches writes two commits to a temp repo and returns the format-patch
// files for them plus their subject labels.
func realPatches(t *testing.T) ([]string, []string) {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "a@b.com")
	run("config", "user.name", "A")
	for _, msg := range []string{"first change", "second change", "third change"} {
		if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "f.txt")
		run("commit", "-q", "-m", msg)
	}
	patchDir := t.TempDir()
	if err := git.FormatPatch(repo, "HEAD~2..HEAD", patchDir); err != nil {
		t.Fatal(err)
	}
	patches, err := git.PatchFiles(patchDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("got %d patches, want 2", len(patches))
	}
	names := patchLabels(patches)
	for i, n := range names {
		if n == "" || strings.HasPrefix(n, "[PATCH") {
			t.Fatalf("patch %d label %q not stripped to a subject", i, n)
		}
	}
	return patches, names
}

// TestViewNoPanic renders every screen at a degenerate and a normal terminal
// size and asserts the output is non-empty.
func TestViewNoPanic(t *testing.T) {
	patches, names := realPatches(t)

	screens := []int{scrSetup, scrRange, scrGenerate, scrInstruction, scrRun, scrReview, scrConfirm, scrDone}
	sizes := [][2]int{{0, 0}, {120, 40}}
	for _, scr := range screens {
		for _, wh := range sizes {
			m := testModel()
			m.screen = scr
			m.width, m.height = wh[0], wh[1]
			if scr == scrReview {
				m.patches = patches
				m.patchNames = names
			}
			m.resize()
			if got := m.viewContent(); strings.TrimSpace(got) == "" {
				t.Fatalf("viewContent for screen %d at %dx%d is empty", scr, wh[0], wh[1])
			}
		}
	}
}

// TestReviewEditViewRenders checks the review screen renders both the patch
// list pane and the editor pane without panicking.
func TestReviewEditViewRenders(t *testing.T) {
	patches, names := realPatches(t)
	m := testModel()
	m.screen = scrReview
	m.width, m.height = 120, 40
	m.patches = patches
	m.patchNames = names
	m.resize()
	if got := m.viewContent(); !strings.Contains(got, names[0]) {
		t.Fatalf("review view does not list the first patch:\n%s", got)
	}
	m.editing = true
	if got := m.viewContent(); strings.TrimSpace(got) == "" {
		t.Fatal("editing review view is empty")
	}
}

// TestTypingReachesRangeInput verifies that key presses are routed to the
// focused widget on the range screen instead of being consumed by handleKey.
func TestTypingReachesRangeInput(t *testing.T) {
	var m Model
	m.screen = scrRange
	m.rangeInput = textinput.New()
	m.rangeInput.Focus()

	for _, r := range []rune{'a', 'b'} {
		nm, _ := m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		m = nm.(Model)
	}

	if got := m.rangeInput.Value(); got != "ab" {
		t.Fatalf("rangeInput.Value() = %q, want %q", got, "ab")
	}
}

// TestCheckFinalStateMismatch verifies that checkFinalState fails loudly when
// the applied patches do not reproduce the expected tree.
func TestCheckFinalStateMismatch(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "a@b.com")
	run("config", "user.name", "A")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "first")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("hello2"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "second")

	base, err := git.RangeBase(repo, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	patchDir := t.TempDir()
	if err := git.FormatPatch(repo, "HEAD~1..HEAD", patchDir); err != nil {
		t.Fatal(err)
	}
	patches, err := git.PatchFiles(patchDir)
	if err != nil || len(patches) == 0 {
		t.Fatalf("no patches: %v", err)
	}

	// The base tree differs from the tree resulting from applying the patches.
	wrong, err := git.TreeOf(repo, base)
	if err != nil {
		t.Fatal(err)
	}

	m := Model{repo: repo, rangeBase: base, patches: patches}
	err = m.checkFinalState(wrong)
	if err == nil {
		t.Fatal("checkFinalState returned nil for a mismatched tree")
	}
	if !strings.Contains(err.Error(), "final state modified") {
		t.Fatalf("error %q does not mention %q", err.Error(), "final state modified")
	}
}

// TestWheelMovesReviewSelection checks that the wheel scrolls the patch list
// when over the left pane and leaves the selection alone over the diff pane.
func TestWheelMovesReviewSelection(t *testing.T) {
	patches, names := realPatches(t)
	m := testModel()
	m.screen = scrReview
	m.width, m.height = 120, 40
	m.patches = patches
	m.patchNames = names
	m.resize()

	lw, _ := m.reviewPaneWidths()
	te, _ := m.Update(tea.MouseWheelMsg{X: lw / 2, Y: 5, Button: tea.MouseWheelDown})
	m = te.(Model)
	if m.selected != 1 {
		t.Fatalf("selected = %d, want 1 after wheel down in list", m.selected)
	}
	te, _ = m.Update(tea.MouseWheelMsg{X: lw / 2, Y: 5, Button: tea.MouseWheelUp})
	m = te.(Model)
	if m.selected != 0 {
		t.Fatalf("selected = %d, want 0 after wheel up in list", m.selected)
	}
	te, _ = m.Update(tea.MouseWheelMsg{X: lw + 1, Y: 5, Button: tea.MouseWheelDown})
	m = te.(Model)
	if m.selected != 0 {
		t.Fatalf("selected = %d, want 0 after wheel over the diff pane", m.selected)
	}
}

// TestConfirmNamesPushTarget checks the confirm screen names the resolved
// upstream and reports when it could not be resolved.
func TestConfirmNamesPushTarget(t *testing.T) {
	m := testModel()
	m.screen = scrConfirm
	m.width, m.height = 120, 40
	m.resize()
	m.pushRemote = "origin"
	m.pushBranch = "main"
	if got := m.viewContent(); !strings.Contains(got, "origin/main") {
		t.Fatalf("confirm view does not name the push target:\n%s", got)
	}

	m.pushErr = "no upstream"
	if got := m.viewContent(); !strings.Contains(got, "no upstream") || !strings.Contains(got, "n/q abort") {
		t.Fatalf("confirm view does not show the push error and abort hint:\n%s", got)
	}
	if _, cmd := m.handleKey("y"); cmd != nil {
		t.Fatal("confirm started a push while pushErr was set")
	}
}

// TestPushRequiresTarget verifies doPush fails closed when no upstream was
// resolved instead of guessing one at push time.
func TestPushRequiresTarget(t *testing.T) {
	m := testModel()
	msg, ok := m.doPush()().(pushResultMsg)
	if !ok {
		t.Fatal("doPush did not return a pushResultMsg")
	}
	if msg.err == nil {
		t.Fatal("doPush succeeded without a push target")
	}
}

// TestConfirmIgnoredWhilePushing verifies that a second confirmation while a
// push is already in flight does not start another push.
func TestConfirmIgnoredWhilePushing(t *testing.T) {
	m := testModel()
	m.screen = scrConfirm
	m.pushRemote = "origin"
	m.pushBranch = "main"

	nm, cmd := m.handleKey("y")
	if cmd == nil {
		t.Fatal("first confirm did not start a push")
	}
	if !nm.pushing {
		t.Fatal("first confirm did not mark the push in flight")
	}
	nm2, cmd2 := nm.handleKey("y")
	if cmd2 != nil {
		t.Fatal("second confirm started a duplicate push")
	}
	if !nm2.pushing {
		t.Fatal("second confirm cleared the in-flight flag")
	}
	if got := nm2.viewContent(); !strings.Contains(got, "Pushing...") {
		t.Fatalf("confirm view does not show the push in flight:\n%s", got)
	}
}

// TestEscOnRunCancels verifies that esc on the run screen cancels the run,
// lands on the done screen, and keeps the patch directory of the aborted run.
func TestEscOnRunCancels(t *testing.T) {
	m := testModel()
	m.screen = scrRun
	m.patchDir = "/tmp/gitforgery2-keep"
	cancelled := false
	m.runCancel = func() { cancelled = true }

	nm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !cancelled {
		t.Fatal("esc on the run screen did not cancel the run")
	}
	if cmd != nil {
		t.Fatal("esc on the run screen returned a command")
	}
	got := nm.(Model)
	if !got.cancelled {
		t.Fatal("cancelled flag not set")
	}
	if got.screen != scrDone {
		t.Fatalf("screen = %d, want scrDone", got.screen)
	}
	if got.finalMsg != "cancelled" || got.finalErr == nil {
		t.Fatalf("final message = %q, err = %v", got.finalMsg, got.finalErr)
	}
	if got.runCancel != nil {
		t.Fatal("runCancel not cleared after cancel")
	}

	// The run's cancellation error must not overwrite the done screen, and the
	// aborted run's patch directory must be retained.
	after, cmd := got.Update(runResultMsg{patchDir: "/tmp/gitforgery2-keep", err: os.ErrDeadlineExceeded})
	if cmd != nil {
		t.Fatal("cancelled run result returned a command")
	}
	done := after.(Model)
	if done.screen != scrDone || done.finalMsg != "cancelled" {
		t.Fatalf("cancelled run result changed the done screen: screen=%d msg=%q", done.screen, done.finalMsg)
	}
	if done.patchDir != "/tmp/gitforgery2-keep" {
		t.Fatalf("patch dir = %q, want retained", done.patchDir)
	}
	if done.cancelled {
		t.Fatal("cancelled flag not reset after the run result")
	}
}

// TestCtrlCOnRunCancelsAndCleans verifies that Ctrl+C during a run cancels the
// in-flight run, removes the aborted run's patch directory and quits.
func TestCtrlCOnRunCancelsAndCleans(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "patches")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := testModel()
	m.screen = scrRun
	m.patchDir = dir
	cancelled := false
	m.runCancel = func() { cancelled = true }

	nm, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !cancelled {
		t.Fatal("ctrl+c on the run screen did not cancel the run")
	}
	if cmd == nil {
		t.Fatal("ctrl+c on the run screen did not return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c on the run screen did not return a quit command")
	}
	got := nm.(Model)
	if got.runCancel != nil {
		t.Fatal("runCancel not cleared after ctrl+c")
	}
	if got.patchDir != "" {
		t.Fatalf("patch dir = %q, want cleared", got.patchDir)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("patch dir still present after ctrl+c: %v", err)
	}
}

// TestCtrlCIgnoredWhilePushing verifies that Ctrl+C cannot quit while a push
// is in flight.
func TestCtrlCIgnoredWhilePushing(t *testing.T) {
	m := testModel()
	m.pushing = true
	nm, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("ctrl+c returned a command while pushing")
	}
	if got := nm.(Model); !got.pushing {
		t.Fatal("ctrl+c cleared the in-flight push flag")
	}
}

// TestResizePreservesEditingText verifies that resizing the terminal while a
// patch is being edited reflows the widgets without reloading the patch.
func TestResizePreservesEditingText(t *testing.T) {
	patches, names := realPatches(t)
	m := testModel()
	m.screen = scrReview
	m.width, m.height = 120, 40
	m.patches = patches
	m.patchNames = names
	m.resize()

	m.editing = true
	m.loadSelectedPatch()
	m.textarea.SetValue("unsaved edit")
	m.textarea.Focus()

	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got := nm.(Model)
	if got.textarea.Value() != "unsaved edit" {
		t.Fatalf("textarea after resize = %q, want the unsaved edit", got.textarea.Value())
	}
	if !got.editing {
		t.Fatal("resize cleared the editing flag")
	}
	if got.width != 100 || got.height != 30 {
		t.Fatalf("size after resize = %dx%d, want 100x30", got.width, got.height)
	}
}

// TestInvalidRangeStaysInline verifies that a bad range reports the error on
// the range screen instead of ending the flow.
func TestInvalidRangeStaysInline(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "a@b.com")
	run("config", "user.name", "A")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "first")

	m := testModel()
	m.repo = repo
	m.screen = scrRange
	m.rangeInput.Focus()
	m.rangeInput.SetValue("no-such-ref..HEAD")

	nm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("invalid range started a command")
	}
	got := nm.(Model)
	if got.screen != scrRange {
		t.Fatalf("screen = %d, want scrRange", got.screen)
	}
	if got.rangeErr == "" {
		t.Fatal("invalid range did not set rangeErr")
	}
	if got.finalErr != nil {
		t.Fatalf("invalid range ended the flow: %v", got.finalErr)
	}
	if !strings.Contains(got.viewContent(), "invalid range") {
		t.Fatalf("range view does not show the inline error:\n%s", got.viewContent())
	}
}

// TestEscBacksOutOfInstruction verifies that esc on the instruction screen
// returns to the range screen with the typed instruction preserved.
func TestEscBacksOutOfInstruction(t *testing.T) {
	m := testModel()
	m.screen = scrInstruction
	m.instructionStatus = "generated; edit and press enter"
	m.instrInput.SetValue("make it concise")
	m.instrInput.Focus()

	nm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatal("esc on the instruction screen returned a command")
	}
	got := nm.(Model)
	if got.screen != scrRange {
		t.Fatalf("screen = %d, want scrRange", got.screen)
	}
	if got.instrInput.Value() != "make it concise" {
		t.Fatalf("instruction after esc = %q, want preserved", got.instrInput.Value())
	}
	if got.instrInput.Focused() {
		t.Fatal("instruction input still focused after esc")
	}
	if !got.rangeInput.Focused() {
		t.Fatal("range input not focused after esc")
	}
}
