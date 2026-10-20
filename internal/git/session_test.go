package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectSessionStats(t *testing.T) {
	repo := newTestRepo(t)
	// Third commit changes an existing line so the range exercises additions,
	// deletions and multiple files.
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("first changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "third")

	wantBranch, err := Run(repo, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	got, err := CollectSessionStats(repo, "HEAD~2", "HEAD")
	if err != nil {
		t.Fatalf("CollectSessionStats: %v", err)
	}
	if got.Branch != wantBranch {
		t.Fatalf("Branch = %q, want %q", got.Branch, wantBranch)
	}
	if got.Commits != 2 {
		t.Fatalf("Commits = %d, want 2", got.Commits)
	}
	if got.Files != 2 {
		t.Fatalf("Files = %d, want 2", got.Files)
	}
	if got.Additions != 2 {
		t.Fatalf("Additions = %d, want 2", got.Additions)
	}
	if got.Deletions != 1 {
		t.Fatalf("Deletions = %d, want 1", got.Deletions)
	}
}

func TestCollectSessionStatsDetached(t *testing.T) {
	repo := newTestRepo(t)
	gitRun(t, repo, "checkout", "--detach", "HEAD")

	got, err := CollectSessionStats(repo, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("CollectSessionStats: %v", err)
	}
	if got.Branch != "detached" {
		t.Fatalf("Branch = %q, want detached", got.Branch)
	}
	if got.Commits != 1 {
		t.Fatalf("Commits = %d, want 1", got.Commits)
	}
}

func TestParseShortstat(t *testing.T) {
	tests := []struct {
		in                string
		files, adds, dels int
	}{
		{"", 0, 0, 0},
		{" 1 file changed, 3 insertions(+)", 1, 3, 0},
		{" 1 file changed, 2 deletions(-)", 1, 0, 2},
		{" 12 files changed, 40 insertions(+), 7 deletions(-)", 12, 40, 7},
		{" 1 file changed, 1 insertion(+), 1 deletion(-)", 1, 1, 1},
	}
	for _, tc := range tests {
		files, adds, dels := parseShortstat(tc.in)
		if files != tc.files || adds != tc.adds || dels != tc.dels {
			t.Errorf("parseShortstat(%q) = %d,%d,%d want %d,%d,%d",
				tc.in, files, adds, dels, tc.files, tc.adds, tc.dels)
		}
	}
}

func TestPatchStats(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("first changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "third")

	outDir := t.TempDir()
	if err := FormatPatch(repo, "HEAD~2..HEAD", outDir); err != nil {
		t.Fatalf("FormatPatch: %v", err)
	}
	patches, err := PatchFiles(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("got %d patches, want 2", len(patches))
	}

	// Patch order is chronological: b.txt added, then a.txt modified.
	want := []PatchStat{
		{Files: 1, Additions: 1, Deletions: 0},
		{Files: 1, Additions: 1, Deletions: 1},
	}
	for i, path := range patches {
		got, err := PatchStats(path)
		if err != nil {
			t.Fatalf("PatchStats(%s): %v", path, err)
		}
		if got != want[i] {
			t.Fatalf("PatchStats(%s) = %+v, want %+v", filepath.Base(path), got, want[i])
		}
	}
}

func TestPatchStatsMissingFile(t *testing.T) {
	if _, err := PatchStats(filepath.Join(t.TempDir(), "absent.patch")); err == nil {
		t.Fatal("PatchStats on missing file: want error")
	}
}
