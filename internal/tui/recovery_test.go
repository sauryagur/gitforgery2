package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installRejectingHook makes the bare remote refuse every push, so a run can
// exercise the recovery path after a push failure.
func installRejectingHook(t *testing.T, remote string) {
	t.Helper()
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestPushFailureRestoresBackupRef verifies that when the push fails the
// rewrite is rolled back to refs/gitforgery2/original, the patch directory is
// kept for inspection, and the failure is reported to the user.
func TestPushFailureRestoresBackupRef(t *testing.T) {
	fr := flowSetupRepo(t)
	installRejectingHook(t, fr.remote)
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

	if m.finalErr == nil {
		t.Fatal("expected a push failure")
	}
	if got := flowGit(t, fr.dir, "rev-parse", "HEAD"); got != fr.tip {
		t.Fatalf("HEAD %s != original tip %s; backup ref was not restored", got, fr.tip)
	}
	if got := flowGit(t, fr.dir, "rev-parse", "refs/gitforgery2/original"); got != fr.tip {
		t.Fatalf("backup ref %s != original tip %s", got, fr.tip)
	}
	if m.patchDir == "" {
		t.Fatal("patch dir was not kept after push failure")
	}
	if _, err := os.Stat(m.patchDir); err != nil {
		t.Fatalf("patch dir %q does not exist: %v", m.patchDir, err)
	}
	if !strings.Contains(m.finalMsg, "push") {
		t.Fatalf("finalMsg = %q, want it to mention the push failure", m.finalMsg)
	}
	if !strings.Contains(m.finalMsg, "refs/gitforgery2/original") {
		t.Fatalf("finalMsg = %q, want it to mention refs/gitforgery2/original", m.finalMsg)
	}
}

// TestRepromptKeepsPatchDir verifies that a reprompt re-runs the harness over
// the existing patch directory without discarding it, its edits, or the
// already-materialised patch set.
func TestRepromptKeepsPatchDir(t *testing.T) {
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
	if m.patchDir == "" {
		t.Fatal("patch dir is empty after the initial run")
	}

	marker := filepath.Join(m.patchDir, "marker.txt")
	if err := os.WriteFile(marker, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startDir := m.patchDir
	startPatches := len(m.patches)

	m, _ = flowPress(t, m, flowKey('r'))
	if m.screen != scrInstruction {
		t.Fatalf("screen = %d, want instruction", m.screen)
	}
	if v := strings.TrimSpace(m.instrArea.Value()); v == "" {
		t.Fatal("instruction input is empty after reprompt")
	}

	m, cmd = flowStartRun(t, m)
	m = flowDrive(t, m, cmd, func(m Model) bool { return m.screen == scrReview || m.screen == scrDone })
	if m.finalErr != nil {
		t.Fatalf("reprompt failed: %v", m.finalErr)
	}
	if m.screen != scrReview {
		t.Fatalf("screen = %d, want review", m.screen)
	}
	if m.patchDir != startDir {
		t.Fatalf("patch dir changed on reprompt: %q != %q", m.patchDir, startDir)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker %q did not survive the reprompt: %v", marker, err)
	}
	if got := len(m.patches); got != startPatches {
		t.Fatalf("patch count changed on reprompt: %d != %d", got, startPatches)
	}
}
