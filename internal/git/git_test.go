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
