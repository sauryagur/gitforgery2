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

	"github.com/sauryagur/gitforgery2/internal/gitx"
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
	if err := gitx.FormatPatch(repo, "HEAD~2..HEAD", patchDir); err != nil {
		t.Fatal(err)
	}
	patches, err := gitx.PatchFiles(patchDir)
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

	base, err := gitx.RangeBase(repo, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	patchDir := t.TempDir()
	if err := gitx.FormatPatch(repo, "HEAD~1..HEAD", patchDir); err != nil {
		t.Fatal(err)
	}
	patches, err := gitx.PatchFiles(patchDir)
	if err != nil || len(patches) == 0 {
		t.Fatalf("no patches: %v", err)
	}

	// The base tree differs from the tree resulting from applying the patches.
	wrong, err := gitx.TreeOf(repo, base)
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
