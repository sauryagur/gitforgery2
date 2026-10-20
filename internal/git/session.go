package git

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// SessionStats summarizes the work in a commit range for the session header.
// Branch falls back to "detached" when HEAD does not name a branch.
type SessionStats struct {
	Branch    string
	Commits   int
	Files     int
	Additions int
	Deletions int
}

// shortstatRe matches `git diff --shortstat` output such as
// " 3 files changed, 10 insertions(+), 2 deletions(-)". The insertion and
// deletion clauses are optional: git omits a clause whose count is zero.
var shortstatRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// CollectSessionStats reports the branch name and the commit, file and line
// counts for base..end. It is the constructor for SessionStats; the type
// occupies the natural name because Go forbids a type and function sharing an
// identifier.
func CollectSessionStats(repo, base, end string) (SessionStats, error) {
	stats := SessionStats{Branch: "detached"}
	if out, err := Run(repo, "symbolic-ref", "--short", "HEAD"); err == nil {
		if branch := strings.TrimSpace(out); branch != "" {
			stats.Branch = branch
		}
	}

	out, err := Run(repo, "rev-list", "--count", base+".."+end)
	if err != nil {
		return stats, err
	}
	commits, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return stats, fmt.Errorf("parse commit count %q: %w", out, err)
	}
	stats.Commits = commits

	out, err = Run(repo, "diff", "--shortstat", base, end)
	if err != nil {
		return stats, err
	}
	stats.Files, stats.Additions, stats.Deletions = parseShortstat(out)
	return stats, nil
}

// parseShortstat extracts file, insertion and deletion counts from
// `git diff --shortstat`. Empty output means the trees are identical.
func parseShortstat(out string) (files, additions, deletions int) {
	m := shortstatRe.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, 0
	}
	files, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		additions, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		deletions, _ = strconv.Atoi(m[3])
	}
	return files, additions, deletions
}

// PatchStat counts the files and changed lines in a single patch file.
type PatchStat struct {
	Files     int
	Additions int
	Deletions int
}

// PatchStats counts files, additions and deletions in a format-patch file.
// A patch header (`diff --git`) opens a file; `+++`/`---` header lines are
// not counted as content.
func PatchStats(path string) (PatchStat, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PatchStat{}, err
	}
	var stat PatchStat
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			stat.Files++
		case line == "--" || strings.HasPrefix(line, "-- "):
			// format-patch signature separator ("-- " + version), not content.
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			// File headers, not added/removed content.
		case strings.HasPrefix(line, "+"):
			stat.Additions++
		case strings.HasPrefix(line, "-"):
			stat.Deletions++
		}
	}
	return stat, nil
}
