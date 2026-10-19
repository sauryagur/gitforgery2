package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPushAbortsWhenHEADMovedAfterReview verifies that a commit landing after
// the run started aborts the push before any reset, so the new commit survives.
func TestPushAbortsWhenHEADMovedAfterReview(t *testing.T) {
	fr := flowSetupRepo(t)
	flowFakeHarness(t, "sed -i 's/^Subject: .*/Subject: rewritten/' ./*.patch")

	m := flowModel(fr.dir, fr.base)
	m, cmd := flowPress(t, m, flowEnter())
	m = flowDrive(t, m, cmd, func(m Model) bool { return m.screen == scrReview || m.screen == scrDone })
	if m.finalErr != nil {
		t.Fatalf("run failed: %v", m.finalErr)
	}
	if m.screen != scrReview {
		t.Fatalf("screen = %d, want review", m.screen)
	}

	if err := os.WriteFile(filepath.Join(fr.dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	flowGit(t, fr.dir, "add", "new.txt")
	flowGit(t, fr.dir, "commit", "-q", "-m", "commit after review")
	newTip := flowGit(t, fr.dir, "rev-parse", "HEAD")

	m, _ = flowPress(t, m, flowKey('a'))
	m, cmd = flowPress(t, m, flowKey('y'))
	m = flowDrive(t, m, cmd, func(m Model) bool { return m.screen == scrDone })

	if m.finalErr == nil {
		t.Fatal("expected a HEAD-moved error")
	}
	if !strings.Contains(m.finalErr.Error(), "HEAD moved") {
		t.Fatalf("error %q does not mention HEAD moved", m.finalErr)
	}
	if got := flowGit(t, fr.dir, "rev-parse", "HEAD"); got != newTip {
		t.Fatalf("HEAD was reset: %s != %s", got, newTip)
	}
	if got := flowGit(t, fr.dir, "rev-parse", "refs/gitforgery2/original"); got != fr.tip {
		t.Fatalf("backup ref %s != %s", got, fr.tip)
	}
}
