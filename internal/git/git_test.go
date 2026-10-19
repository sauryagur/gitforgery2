package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestLog(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test User")

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "first\n")
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "first")
	write("b.txt", "second\n")
	gitRun(t, repo, "add", "b.txt")
	gitRun(t, repo, "commit", "-m", "second")

	out, err := Log(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"first", "second"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log missing %q:\n%s", want, out)
		}
	}
}

func TestDefaultRangeClampsToHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test User")

	commitN := func(n int) {
		for i := 1; i <= n; i++ {
			name := filepath.Join(repo, "f.txt")
			if err := os.WriteFile(name, []byte(strings.Repeat("x", i)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, repo, "add", "f.txt")
			gitRun(t, repo, "commit", "-m", "commit")
		}
	}
	commitN(3)
	got, err := DefaultRange(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "HEAD~2..HEAD" {
		t.Fatalf("DefaultRange with 3 commits = %q, want HEAD~2..HEAD", got)
	}
	commitN(5)
	got, err = DefaultRange(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "HEAD~5..HEAD" {
		t.Fatalf("DefaultRange with 8 commits = %q, want HEAD~5..HEAD", got)
	}
}

func TestDefaultRangeSkipsSyncedUpstream(t *testing.T) {
	repo := newTestRepo(t)
	bare := t.TempDir()
	gitRun(t, repo, "init", "--bare", "-q", bare)
	gitRun(t, repo, "remote", "add", "origin", bare)
	gitRun(t, repo, "push", "-q", "-u", "origin", "HEAD")

	got, err := DefaultRange(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "HEAD~1..HEAD" {
		t.Fatalf("DefaultRange with synced upstream = %q, want HEAD~1..HEAD", got)
	}
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test User")
	commit := func(name, content, msg string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, repo, "add", name)
		gitRun(t, repo, "commit", "-m", msg)
	}
	commit("a.txt", "first\n", "first")
	commit("b.txt", "second\n", "second")
	return repo
}

func TestParseRange(t *testing.T) {
	repo := newTestRepo(t)

	wantBase, err := Run(repo, "rev-parse", "--verify", "HEAD~1^{commit}")
	if err != nil {
		t.Fatal(err)
	}
	wantEnd, err := Run(repo, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		t.Fatal(err)
	}

	base, end, err := ParseRange(repo, "HEAD~1..HEAD")
	if err != nil {
		t.Fatalf("ParseRange: %v", err)
	}
	if base != wantBase || end != wantEnd {
		t.Fatalf("ParseRange = %q..%q, want %q..%q", base, end, wantBase, wantEnd)
	}

	base, _, err = ParseRange(repo, "HEAD")
	if err == nil {
		t.Fatalf("ParseRange(HEAD) = %q, want error", base)
	}

	tests := []struct {
		name string
		spec string
	}{
		{"three-dot", "HEAD~1...HEAD"},
		{"no separator", "HEAD"},
		{"empty base", "..HEAD"},
		{"empty end", "HEAD.."},
		{"both empty", ".."},
		{"nonexistent base", "no-such-rev..HEAD"},
		{"nonexistent end", "HEAD..no-such-rev"},
		{"empty range", "HEAD..HEAD"},
		{"empty range via aliases", "HEAD..HEAD^{commit}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := ParseRange(repo, tt.spec); err == nil {
				t.Fatalf("ParseRange(%q) succeeded, want error", tt.spec)
			}
		})
	}

	if _, err := RangeBase(repo, "HEAD~1..HEAD"); err != nil {
		t.Fatalf("RangeBase: %v", err)
	}
}

func TestParseRangeRejectsMerges(t *testing.T) {
	repo := newTestRepo(t)

	gitRun(t, repo, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(repo, "side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "side.txt")
	gitRun(t, repo, "commit", "-m", "side commit")

	gitRun(t, repo, "checkout", "-")
	if err := os.WriteFile(filepath.Join(repo, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "main.txt")
	gitRun(t, repo, "commit", "-m", "main commit")
	gitRun(t, repo, "merge", "--no-ff", "side", "-m", "merge side")

	// HEAD~1..HEAD contains only the merge commit itself.
	if _, _, err := ParseRange(repo, "HEAD~1..HEAD"); err == nil {
		t.Fatal("ParseRange on a merge range succeeded, want error")
	} else if !strings.Contains(err.Error(), "merge") {
		t.Fatalf("ParseRange merge error = %v, want mention of merge commits", err)
	}

	// A linear range ending before the merge is still accepted.
	if _, _, err := ParseRange(repo, "HEAD~3..HEAD~1"); err != nil {
		t.Fatalf("ParseRange on linear range: %v", err)
	}
}

func TestCreateBackupRefWriteOnce(t *testing.T) {
	repo := newTestRepo(t)
	tip, err := CurrentTip(repo)
	if err != nil {
		t.Fatal(err)
	}

	if err := CreateBackupRef(repo); err != nil {
		t.Fatalf("first CreateBackupRef: %v", err)
	}
	got, err := Run(repo, "rev-parse", "refs/gitforgery2/original")
	if err != nil {
		t.Fatal(err)
	}
	if got != tip {
		t.Fatalf("backup ref = %s, want %s", got, tip)
	}

	// Same tip: no-op.
	if err := CreateBackupRef(repo); err != nil {
		t.Fatalf("CreateBackupRef on same tip: %v", err)
	}

	// New commit: refuse to overwrite the original backup.
	if err := os.WriteFile(filepath.Join(repo, "c.txt"), []byte("third\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "c.txt")
	gitRun(t, repo, "commit", "-m", "third")
	if err := CreateBackupRef(repo); err == nil {
		t.Fatal("CreateBackupRef after new commit succeeded, want error")
	} else if !strings.Contains(err.Error(), tip) {
		t.Fatalf("CreateBackupRef error = %v, want mention of original tip %s", err, tip)
	}
	got, err = Run(repo, "rev-parse", "refs/gitforgery2/original")
	if err != nil {
		t.Fatal(err)
	}
	if got != tip {
		t.Fatalf("backup ref changed to %s after refusal, want %s", got, tip)
	}
}

func TestDefaultRangeSingleCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "first")

	got, err := DefaultRange(repo)
	if err == nil {
		t.Fatalf("DefaultRange on 1-commit repo = %q, want error", got)
	}
	if got != "" {
		t.Fatalf("DefaultRange on 1-commit repo returned %q with error, want empty", got)
	}
	if !strings.Contains(err.Error(), "at least 2 commits") {
		t.Fatalf("DefaultRange error = %v, want mention of 2 commits", err)
	}
}

func TestCleanWorktree(t *testing.T) {
	repo := newTestRepo(t)

	if err := CleanWorktree(repo); err != nil {
		t.Fatalf("CleanWorktree on clean repo: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CleanWorktree(repo); err != nil {
		t.Fatalf("CleanWorktree with only untracked file: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CleanWorktree(repo); err == nil {
		t.Fatal("CleanWorktree with modified tracked file succeeded, want error")
	}
}
