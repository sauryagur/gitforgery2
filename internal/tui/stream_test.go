package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestStreamCommandSeparatesStreams verifies stdout and stderr are captured
// separately and streamed to the channel with stderr lines prefixed.
func TestStreamCommandSeparatesStreams(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-harness.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho out1\necho err1 >&2\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ch := make(chan tea.Msg, 16)
	stdout, stderr, err := streamCommand(context.Background(), script, nil, dir, ch)
	close(ch)
	if err != nil {
		t.Fatalf("streamCommand: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != "out1" {
		t.Fatalf("stdout = %q, want %q", got, "out1")
	}
	if got := strings.TrimSpace(stderr); got != "err1" {
		t.Fatalf("stderr = %q, want %q", got, "err1")
	}

	var lines []string
	for msg := range ch {
		out, ok := msg.(harnessOutputMsg)
		if !ok {
			t.Fatalf("unexpected message type %T", msg)
		}
		lines = append(lines, out.line)
	}
	if want := []string{"out1", "stderr: err1"}; !sameLines(lines, want) {
		t.Fatalf("streamed lines = %v, want %v", lines, want)
	}
}

// sameLines reports whether got has exactly the elements of want, regardless
// of order (stdout and stderr are read concurrently).
func sameLines(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(want))
	for _, line := range want {
		seen[line]++
	}
	for _, line := range got {
		if seen[line] == 0 {
			return false
		}
		seen[line]--
	}
	return true
}
