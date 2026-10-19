package harness

import (
	"strings"
	"testing"
)

func TestInstructionPrompt(t *testing.T) {
	log := "abc123 2026-10-19 Test <t@example.com>\nfirst\n----"
	p, err := InstructionPrompt(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, log) {
		t.Fatalf("prompt does not embed log:\n%s", p)
	}
	if !strings.Contains(p, "instruction") {
		t.Fatalf("prompt does not mention instruction:\n%s", p)
	}
}
