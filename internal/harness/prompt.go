package harness

import (
	"strings"
	"text/template"
)

const instructionPromptTemplate = `You are preparing an instruction for a patch-editing agent that will rewrite a
repository's git history by editing generated git format-patch files.

Read the git log below and write a concise plain-text instruction (2-4 sentences,
no markdown, no preamble) for that agent. Cover commit message quality, and say
that the final tree must stay unchanged. Where it is obvious, tell it to group or
split commits. Do not address the user directly and do not add commentary.

Output only the instruction text.

Git log:
{{.Log}}`

type instructionPromptData struct {
	Log string
}

// InstructionPrompt renders the prompt that asks a harness to turn a git log
// into a plain-text history-rewrite instruction.
func InstructionPrompt(log string) (string, error) {
	t, err := template.New("instruction").Parse(instructionPromptTemplate)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, instructionPromptData{Log: log}); err != nil {
		return "", err
	}
	return b.String(), nil
}
