package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sauryagur/gitforgery2/internal/gitx"
)

func dir(repo string) string {
	gd, err := gitx.GitDir(repo)
	if err != nil || gd == "" {
		gd = filepath.Join(repo, ".git")
	}
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(repo, gd)
	}
	return filepath.Join(gd, "gitforgery2")
}

type Snapshot struct {
	Tip  string `json:"tip"`
	Tree string `json:"tree"`
}



func path(repo string) string {
	return filepath.Join(dir(repo), "snapshot")
}

func Save(repo string, s Snapshot) error {
	d := dir(repo)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(repo), b, 0o644)
}

func Load(repo string) (*Snapshot, error) {
	b, err := os.ReadFile(path(repo))
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Tip == "" || s.Tree == "" {
		return nil, fmt.Errorf("invalid snapshot")
	}
	return &s, nil
}
