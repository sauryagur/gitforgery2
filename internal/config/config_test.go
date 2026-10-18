package config

import (
	"testing"
)

func TestSaveLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	c := &Config{Harness: "codex"}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Harness != "codex" {
		t.Fatalf("got %s", got.Harness)
	}
}
