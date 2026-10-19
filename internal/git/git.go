package git

import (
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
	// try upstream merge-base
	up, err := Run(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err == nil && up != "" {
		mb, err := Run(repo, "merge-base", "HEAD", up)
		if err == nil && mb != "" {
			return mb + "..HEAD", nil
		}
	}
	// fallback: up to the last 5 commits, clamped to the repo's history so a
	// short repo does not produce an invalid "HEAD~5" range.
	if out, err := Run(repo, "rev-list", "--count", "HEAD"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil && n >= 2 {
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
	args := append([]string{"am"}, patches...)
	_, err := Run(repo, args...)
	return err
}

func CreateBackupRef(repo string) error {
	tip, err := CurrentTip(repo)
	if err != nil {
		return err
	}
	_, err = Run(repo, "update-ref", "refs/gitforgery2/original", tip)
	return err
}

func ForcePush(repo, remote, branch string) error {
	_, err := Run(repo, "push", "--force", remote, "HEAD:"+branch)
	return err
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
	applyArgs := append([]string{"apply", "--cached"}, patches...)
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

func RangeBase(repo, rangeSpec string) (string, error) {
	parts := strings.SplitN(rangeSpec, "..", 2)
	base := strings.TrimSpace(parts[0])
	if base == "" {
		return "", fmt.Errorf("invalid range: %s", rangeSpec)
	}
	out, err := Run(repo, "rev-parse", base+"^{commit}")
	if err != nil {
		out, err = Run(repo, "rev-parse", base)
	}
	return out, err
}
