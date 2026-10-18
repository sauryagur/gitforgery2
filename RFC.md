# RFC: gitforgery2 — AI-assisted history rewriting via patches

Status: accepted (2026-10-19). Supersedes DESIGN.md for the v2 product.

## 1. Summary

`gitforgery2` is a lightweight, fullscreen Terminal UI tool that rewrites git
history by converting a commit range into `git format-patch` files, letting an
existing harness on the user's machine (pi, codex, or claude code) edit those
patches according to a user instruction, then replaying them into incremental
commits and force-pushing. A "Don't modify final state" option guarantees the
final tree is byte-identical to the original tip.

## 2. Goals / non-goals

Goals:
- Single-purpose, minimal: one binary, one TUI, no subcommands.
- Reuse the user's already-authenticated harness (no API keys in the tool).
- Preserve authorship/dates where the harness leaves them; final tree equality
  when requested.
- Reversible locally via a backup ref.

Non-goals: metadata-only recipe rewrites (v1 feature, dropped), fabricate,
audit, re-sign, MCP server, Go-import-graph inference.

## 3. Architecture

```
main.go -> tui.Run()
internal/
  config/    JSON config at ~/.config/gitforgery2/config.json
  git/       os/exec wrappers: format-patch, am, apply --check, tree OID,
             backup ref, force-push, range resolution
  harness/   Adapter interface + pi/codex/claude implementations
  snapshot/  per-repo snapshot at $GIT_DIR/gitforgery2/snapshot
  tui/       bubbletea v2 model + screens
```

Dependencies: bubbletea v2, bubbles v2, lipgloss v2, stdlib only.

## 4. Flow

Setup -> Range select -> Instruction input -> Run (format-patch + harness subprocess)
-> Review/Edit (textarea, reprompt, accept/abort) -> Confirm push (explicit) -> force-push.

## 5. "Don't modify final state"

Checkbox: before final commit, `git apply --check` all patches and compare the
resulting tree OID to the snapshot's tree OID; on mismatch abort, keep temp dir,
print diff summary. No force-push.

## 6. Config & harness detection

Detect pi/codex/claude on PATH. Auto-select if one; prompt if multiple; error
with install hint if none. Persist selection + version to config.

## 7. Testing & evals

Go tests: unit + integration against real temp git repos with a fake harness
binary. Evals: deterministic round-trip scenarios verifying patch fidelity and
final-state enforcement.

## 8. Minimalism

No cobra, no go-git, no yaml. Git CLI is the only git implementation. Config is
JSON via encoding/json.
