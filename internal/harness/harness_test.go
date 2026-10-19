package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeExecutable creates an executable fake harness in dir.
func writeExecutable(t *testing.T, dir, name, script string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

// prependPath puts dir first on PATH while keeping the real PATH so the test
// still works when the environment changes.
func prependPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestNew(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"pi", "pi"},
		{"codex", "codex"},
		{"claude", "claude"},
	}
	for _, tt := range tests {
		h, err := New(tt.name)
		if err != nil {
			t.Fatalf("New(%q): %v", tt.name, err)
		}
		if got := h.Name(); got != tt.want {
			t.Errorf("New(%q).Name() = %q, want %q", tt.name, got, tt.want)
		}
	}

	if _, err := New("unknown"); err == nil {
		t.Fatal("New(unknown) returned no error")
	}
}

func TestCommandShapes(t *testing.T) {
	tests := []struct {
		name string
		h    Harness
		bin  string
	}{
		{"pi", Pi{}, "pi"},
		{"codex", Codex{}, "codex"},
		{"claude", Claude{}, "claude"},
	}

	const prompt = "improve commit messages"
	for _, tt := range tests {
		bin, args := tt.h.Command(prompt)
		if bin != tt.bin {
			t.Errorf("%s Command binary = %q, want %q", tt.name, bin, tt.bin)
		}
		if !strings.Contains(strings.Join(args, " "), prompt) {
			t.Errorf("%s Command args %q do not contain prompt %q", tt.name, args, prompt)
		}
	}
}

func TestDetect(t *testing.T) {
	t.Run("finds installed harnesses", func(t *testing.T) {
		dir := t.TempDir()
		writeExecutable(t, dir, "pi", "#!/bin/sh\nexit 0\n")
		writeExecutable(t, dir, "codex", "#!/bin/sh\nexit 0\n")
		prependPath(t, dir)

		got := Detect()
		found := make(map[string]bool, len(got))
		for _, name := range got {
			found[name] = true
		}
		for _, want := range []string{"pi", "codex"} {
			if !found[want] {
				t.Errorf("Detect() = %v, want it to include %q", got, want)
			}
		}
	})

	t.Run("empty PATH finds none", func(t *testing.T) {
		// No real PATH: only the empty temp dir is searched.
		t.Setenv("PATH", t.TempDir())
		if got := Detect(); len(got) != 0 {
			t.Fatalf("Detect() = %v, want none", got)
		}
	})
}

func TestVersion(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, dir, "pi", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 1.2.3; fi\n")
	prependPath(t, dir)

	if got := Version("pi"); got != "1.2.3" {
		t.Fatalf("Version(pi) = %q, want %q", got, "1.2.3")
	}
}
