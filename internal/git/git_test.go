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
