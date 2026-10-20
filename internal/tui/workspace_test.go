package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sauryagur/gitforgery2/internal/git"
)

// TestParseDiffLinesAndWindow checks that unified diff lines are classified
// for the renderer and that diffWindow clamps its offset to the content.
func TestParseDiffLinesAndWindow(t *testing.T) {
	text := "diff --git a/f.txt b/f.txt\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/f.txt\n" +
		"+++ b/f.txt\n" +
		"@@ -1,2 +1,2 @@\n" +
		" context\n" +
		"-old\n" +
		"+new\n"

	lines := parseDiffLines(text)
	want := []diffKind{diffHeader, diffHeader, diffHeader, diffHeader, diffHunk, diffContext, diffDel, diffAdd}
	if len(lines) != len(want) {
		t.Fatalf("parseDiffLines returned %d lines, want %d: %#v", len(lines), len(want), lines)
	}
	for i, kind := range want {
		if lines[i].Kind != kind {
			t.Fatalf("line %d kind = %d, want %d (%q)", i, lines[i].Kind, kind, lines[i].Text)
		}
	}
	if lines[len(lines)-1].Text != "+new" {
		t.Fatalf("last line text = %q, want %q", lines[len(lines)-1].Text, "+new")
	}

	visible, total := diffWindow(lines, 7, 2)
	if total != len(lines) {
		t.Fatalf("diffWindow total = %d, want %d", total, len(lines))
	}
	if len(visible) != 2 {
		t.Fatalf("diffWindow returned %d visible lines, want 2", len(visible))
	}
	if visible[0].Text != "-old" || visible[1].Text != "+new" {
		t.Fatalf("diffWindow did not clamp the offset: %#v", visible)
	}

	visible, total = diffWindow(lines, -4, 2)
	if total != len(lines) || len(visible) != 2 || visible[0].Kind != diffHeader {
		t.Fatalf("diffWindow did not clamp a negative offset: total=%d visible=%#v", total, visible)
	}

	if visible, total := diffWindow(lines, 0, 0); visible != nil || total != len(lines) {
		t.Fatalf("diffWindow with zero height = %#v, %d", visible, total)
	}
}

// TestWorkspaceReflectsState verifies workspace() copies the run state the
// shell and panels render.
func TestWorkspaceReflectsState(t *testing.T) {
	dir := t.TempDir()
	patches := make([]string, 0, 2)
	for _, name := range []string{"0001-first.patch", "0002-second.patch"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("Subject: [PATCH] "+name+"\n\n@@ -1 +1 @@\n-a\n+b\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		patches = append(patches, path)
	}

	m := testModel()
	m.screen = scrReview
	m.session = git.SessionStats{Branch: "main", Commits: 2, Files: 3, Additions: 4, Deletions: 5}
	m.step = stepReview
	m.stepStatus[stepRange] = stepDone
	m.stepStatus[stepReview] = stepActive
	m.logs = []logLine{{At: "12:00:00", Stage: "export", Text: "line"}}
	m.patches = patches
	m.selected = 1
	m.dontModify = true
	m.elapsed = "2s"
	m.instruction = "rewrite history"

	v := m.workspace()
	if v.Branch != "main" || v.Commits != 2 || v.Files != 3 || v.Additions != 4 || v.Deletions != 5 {
		t.Fatalf("session not copied: %+v", v)
	}
	if v.Step != stepReview || v.StepStatus[stepRange] != stepDone || v.StepStatus[stepReview] != stepActive {
		t.Fatalf("step state not copied: step=%d status=%v", v.Step, v.StepStatus)
	}
	if len(v.Logs) != 1 || v.Logs[0].Text != "line" || v.Logs[0].Stage != "export" {
		t.Fatalf("logs not copied: %#v", v.Logs)
	}
	if v.PatchCount != 2 || len(v.Patches) != 2 || v.Selected != 1 {
		t.Fatalf("patches not copied: count=%d items=%d selected=%d", v.PatchCount, len(v.Patches), v.Selected)
	}
	if v.Patches[0].Title != "0001-first.patch" {
		t.Fatalf("patch title = %q, want the stripped subject", v.Patches[0].Title)
	}
	if v.FinalTreeMatch != m.dontModify {
		t.Fatalf("FinalTreeMatch = %v, want %v", v.FinalTreeMatch, m.dontModify)
	}
	if v.Elapsed != "2s" || v.Instruction != "rewrite history" {
		t.Fatalf("elapsed/instruction not copied: %q/%q", v.Elapsed, v.Instruction)
	}
}

// TestHarnessLogAndElapsed verifies streamed output lands in the log and the
// elapsed timer updates the workspace view.
func TestHarnessLogAndElapsed(t *testing.T) {
	m := testModel()
	nm, cmd := m.Update(harnessOutputMsg{at: time.Now(), stage: "export", line: "x"})
	if cmd != nil {
		t.Fatalf("harness output without a channel returned a command")
	}
	m = nm.(Model)
	if len(m.logs) != 1 || m.logs[0].Stage != "export" || m.logs[0].Text != "x" {
		t.Fatalf("logs = %#v, want one export line", m.logs)
	}
	if got := m.workspace().Logs; len(got) != 1 || got[0].Text != "x" {
		t.Fatalf("workspace logs = %#v, want the streamed line", got)
	}

	m.screen = scrRun
	m.runStarted = time.Now().Add(-2 * time.Second)
	nm, _ = m.Update(elapsedMsg{})
	m = nm.(Model)
	if m.elapsed == "" {
		t.Fatal("elapsedMsg did not set elapsed")
	}
	if got := m.workspace().Elapsed; got == "" {
		t.Fatal("workspace elapsed is empty after elapsedMsg")
	}
}

// TestHelpToggle verifies ? opens the help overlay and esc closes it before
// the active screen sees the key.
func TestHelpToggle(t *testing.T) {
	m := testModel()
	m.screen = scrReview
	m.width, m.height = 160, 50

	nm, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = nm.(Model)
	if !m.helpVisible {
		t.Fatal("? did not open help")
	}
	if got := m.viewContent(); !strings.Contains(got, "Help") {
		t.Fatalf("help view does not contain the overlay title:\n%s", got)
	}

	nm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = nm.(Model)
	if m.helpVisible {
		t.Fatal("esc did not close help")
	}
}

// TestOriginalToggle verifies o swaps the diff between the edited patch and
// its pre-run copy.
func TestOriginalToggle(t *testing.T) {
	patchDir := t.TempDir()
	originalDir := t.TempDir()
	working := filepath.Join(patchDir, "0001-one.patch")
	original := filepath.Join(originalDir, filepath.Base(working))
	edited := "Subject: [PATCH] one\n\n@@ -1 +1 @@\n-old\n+edited\n"
	originalText := "Subject: [PATCH] one\n\n@@ -1 +1 @@\n-old\n+original\n"
	if err := os.WriteFile(working, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte(originalText), 0o644); err != nil {
		t.Fatal(err)
	}

	m := testModel()
	m.screen = scrReview
	m.patches = []string{working}
	m.originalDir = originalDir
	m.selected = 0
	m.reloadDiff()
	before := m.diffLines
	if len(before) == 0 || !strings.Contains(before[len(before)-1].Text, "edited") {
		t.Fatalf("initial diff = %#v, want the working patch", before)
	}

	nm, _ := m.Update(tea.KeyPressMsg{Text: "o", Code: 'o'})
	m = nm.(Model)
	if !m.showOriginal {
		t.Fatal("o did not switch to the original patch")
	}
	if len(m.diffLines) == 0 || m.diffLines[len(m.diffLines)-1].Text == before[len(before)-1].Text {
		t.Fatalf("diff did not change after toggling original: %#v", m.diffLines)
	}
	if !strings.Contains(m.diffLines[len(m.diffLines)-1].Text, "original") {
		t.Fatalf("original diff = %#v, want the pre-run copy", m.diffLines)
	}
}
