package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := Snapshot{Tip: "abc", Tree: "def"}
	if err := Save(repo, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tip != "abc" || got.Tree != "def" {
		t.Fatalf("got %+v", got)
	}
}
