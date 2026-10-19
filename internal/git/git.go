package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func RepoRoot(dir string) (string, error) {
	return Run(dir, "rev-parse", "--show-toplevel")
}

func GitDir(repo string) (string, error) {
	return Run(repo, "rev-parse", "--git-dir")
}

func CurrentTip(repo string) (string, error) {
	return Run(repo, "rev-parse", "HEAD")
}

func TreeOf(repo, ref string) (string, error) {
	return Run(repo, "rev-parse", ref+"^{tree}")
}

func DefaultRange(repo string) (string, error) {
	// Use the upstream merge-base only when HEAD is actually ahead of it;
	// otherwise a synced branch yields an empty "HEAD..HEAD" range.
	tip, tipErr := CurrentTip(repo)
	up, err := Run(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err == nil && up != "" {
		mb, err := Run(repo, "merge-base", "HEAD", up)
		if err == nil && mb != "" && (tipErr != nil || mb != tip) {
			return mb + "..HEAD", nil
		}
	}
	// A repository without commits has no range to rewrite; report that
	// clearly instead of a raw rev-parse failure.
	if _, err := Run(repo, "rev-parse", "--verify", "HEAD"); err != nil {
		return "", fmt.Errorf("repository has no commits yet")
	}
	// fallback: up to the last 5 commits, clamped to the repo's history so a
	// short repo does not produce an invalid "HEAD~5" range.
	if out, err := Run(repo, "rev-list", "--count", "HEAD"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			if n < 2 {
				return "", fmt.Errorf("repository needs at least 2 commits to rewrite a range")
			}
			k := n - 1
			if k > 5 {
				k = 5
			}
			return fmt.Sprintf("HEAD~%d..HEAD", k), nil
		}
	}
	return "HEAD~5..HEAD", nil
}

// Log returns the git log for rangeSpec formatted for prompt input. The
// --max-count=200 cap keeps large ranges from producing unbounded output.
func Log(repo, rangeSpec string) (string, error) {
	if strings.TrimSpace(rangeSpec) == "" {
		rangeSpec = "HEAD"
	}
	return Run(repo, "log", "--no-merges", "--max-count=200", "--date=iso",
		"--format=%h %ad %an <%ae>%n%s%n%b%n----", rangeSpec)
}

func FormatPatch(repo, rangeSpec, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	_, err := Run(repo, "format-patch", rangeSpec, "-o", outDir)
	return err
}

func PatchFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".patch") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// tempIndex creates a temporary index seeded from base and returns its
// GIT_INDEX_FILE environment plus a cleanup func.
func tempIndex(repo, base string) ([]string, func(), error) {
	f, err := os.CreateTemp("", "gitforgery2-index-*")
	if err != nil {
		return nil, nil, err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return nil, nil, err
	}
	env := append(os.Environ(), "GIT_INDEX_FILE="+name)
	read := exec.Command("git", "read-tree", base)
	read.Dir = repo
	read.Env = env
	if out, err := read.CombinedOutput(); err != nil {
		_ = os.Remove(name)
		return nil, nil, fmt.Errorf("read-tree: %w\n%s", err, string(out))
	}
	return env, func() { _ = os.Remove(name) }, nil
}

func Am(repo string, patches []string) error {
	if len(patches) == 0 {
		return nil
	}
	args := append([]string{"am", "--empty=keep"}, patches...)
	_, err := Run(repo, args...)
	return err
}

// CreateBackupRef records the current tip under refs/gitforgery2/original.
// The ref is write-once per original tip: an existing ref is a no-op when it
// already names the tip, and an error otherwise so earlier history is never
// silently overwritten.
func CreateBackupRef(repo string) error {
	tip, err := CurrentTip(repo)
	if err != nil {
		return err
	}
	existing, err := Run(repo, "rev-parse", "--verify", "--quiet", "refs/gitforgery2/original")
	if err == nil && existing != "" {
		if existing == tip {
			return nil
		}
		return fmt.Errorf("backup ref refs/gitforgery2/original already points to %s, but current tip is %s; delete or move the ref before rewriting again", existing, tip)
	}
	_, err = Run(repo, "update-ref", "refs/gitforgery2/original", tip)
	return err
}

func ForcePush(ctx context.Context, repo, remote, branch string) error {
	args := []string{"push", "--force", remote, "HEAD:" + branch}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func RemoteBranch(repo string) (string, string, error) {
	up, err := Run(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return "", "", err
	}
	// up like origin/main
	parts := strings.SplitN(up, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected upstream: %s", up)
	}
	return parts[0], parts[1], nil
}

// DiffStat returns the trimmed `git diff --stat` output between two trees.
func DiffStat(repo, oldTree, newTree string) (string, error) {
	return Run(repo, "diff", "--stat", oldTree, newTree)
}

// ResultingTree applies patches to a temporary index seeded from base and
// returns the resulting tree OID. The apply both validates the series and
// yields the tree used by the RFC §5 final-state check; `git apply --check`
// cannot validate a sequential format-patch series in one invocation.
func ResultingTree(repo, base string, patches []string) (string, error) {
	if base == "" {
		base = "HEAD"
	}
	if len(patches) == 0 {
		tree, err := TreeOf(repo, base)
		return tree, err
	}
	env, cleanup, err := tempIndex(repo, base)
	if err != nil {
		return "", err
	}
	defer cleanup()
	applyArgs := append([]string{"apply", "--cached", "--allow-empty"}, patches...)
	apply := exec.Command("git", applyArgs...)
	apply.Dir = repo
	apply.Env = env
	if out, err := apply.CombinedOutput(); err != nil {
		return "", fmt.Errorf("apply --cached: %w\n%s", err, string(out))
	}
	write := exec.Command("git", "write-tree")
	write.Dir = repo
	write.Env = env
	out, err := write.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("write-tree: %w\n%s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// CleanWorktree reports an error when tracked files have uncommitted changes.
// Untracked files alone do not block a rewrite, so they are ignored.
func CleanWorktree(repo string) error {
	out, err := Run(repo, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("working tree has uncommitted changes; commit or stash them before rewriting")
	}
	return nil
}

// ParseRange validates an A..B commit range and returns the commit OIDs for
// both endpoints. Three-dot ranges are rejected because this tool only
// rewrites a linear series with a definite base.
func ParseRange(repo, spec string) (base, end string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", fmt.Errorf("empty range")
	}
	if strings.Contains(spec, "...") {
		return "", "", fmt.Errorf("three-dot ranges are not supported; use A..B instead of %q", spec)
	}
	parts := strings.SplitN(spec, "..", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid range %q: expected A..B", spec)
	}
	base = strings.TrimSpace(parts[0])
	end = strings.TrimSpace(parts[1])
	if base == "" || end == "" {
		return "", "", fmt.Errorf("invalid range %q: both endpoints must be non-empty", spec)
	}
	verify := func(name, rev string) (string, error) {
		out, err := Run(repo, "rev-parse", "--verify", rev+"^{commit}")
		if err != nil {
			return "", fmt.Errorf("invalid range %q: %s endpoint %q: %w", spec, name, rev, err)
		}
		return out, nil
	}
	base, err = verify("base", base)
	if err != nil {
		return "", "", err
	}
	end, err = verify("end", end)
	if err != nil {
		return "", "", err
	}
	if base == end {
		return "", "", fmt.Errorf("range is empty")
	}
	merges, err := Run(repo, "rev-list", "--merges", base+".."+end)
	if err != nil {
		return "", "", fmt.Errorf("checking range for merge commits: %w", err)
	}
	if merges != "" {
		return "", "", fmt.Errorf("range contains merge commits; use a linear range (rebase or --first-parent) before rewriting")
	}
	return base, end, nil
}

func RangeBase(repo, rangeSpec string) (string, error) {
	base, _, err := ParseRange(repo, rangeSpec)
	return base, err
}
