package harness

import (
	"fmt"
	"os/exec"
	"strings"
)

type Harness interface {
	Name() string
	// Command returns the binary and args for a run with the given prompt.
	// The working directory is set by the caller to the patch directory.
	Command(prompt string) (string, []string)
}

type Pi struct{}

func (Pi) Name() string { return "pi" }
func (Pi) Command(prompt string) (string, []string) {
	return "pi", []string{"-p", "--no-session", "--mode", "text", prompt}
}

type Codex struct{}

func (Codex) Name() string { return "codex" }
func (Codex) Command(prompt string) (string, []string) {
	return "codex", []string{"exec", prompt}
}

type Claude struct{}

func (Claude) Name() string { return "claude" }
func (Claude) Command(prompt string) (string, []string) {
	return "claude", []string{"-p", prompt}
}

func Detect() []string {
	var out []string
	for _, name := range []string{"pi", "codex", "claude"} {
		if _, err := exec.LookPath(name); err == nil {
			out = append(out, name)
		}
	}
	return out
}

func Version(name string) string {
	switch name {
	case "pi", "codex", "claude":
	default:
		return ""
	}
	out, err := exec.Command(name, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func New(name string) (Harness, error) {
	switch name {
	case "pi":
		return Pi{}, nil
	case "codex":
		return Codex{}, nil
	case "claude":
		return Claude{}, nil
	default:
		return nil, fmt.Errorf("unknown harness: %s", name)
	}
}
