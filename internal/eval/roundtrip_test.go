package eval

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sauryagur/gitforgery2/internal/git"
)

func TestFormatPatchRoundTrip(t *testing.T) {
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

	treeBefore, _ := git.TreeOf(repo, "HEAD")

	patchDir := t.TempDir()
	if err := git.FormatPatch(repo, "HEAD~1..HEAD", patchDir); err != nil {
		t.Fatal(err)
	}
	patches, err := git.PatchFiles(patchDir)
	if err != nil || len(patches) == 0 {
		t.Fatalf("no patches: %v", err)
	}

	// reset repo to before and re-apply patches
	run("reset", "--hard", "HEAD~1")
	if err := git.Am(repo, patches); err != nil {
		t.Fatal(err)
	}
	treeAfter, _ := git.TreeOf(repo, "HEAD")
	if treeAfter != treeBefore {
		t.Fatalf("tree changed: %s != %s", treeAfter, treeBefore)
	}
}
