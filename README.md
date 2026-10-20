# gitforgery2

Lightweight fullscreen TUI that rewrites git history by turning a commit range
into `git format-patch` files, letting an already-authenticated harness on your
machine (`pi`, `codex`, or `claude`) edit those patches, then replaying them as
incremental commits and force-pushing.

The accepted spec is [RFC.md](RFC.md).

## Requirements

- Go (build only) and the `git` CLI at runtime.
- One of `pi`, `codex`, or `claude` on `PATH`. If several are installed, the
  TUI asks which to use; the choice and its version are saved to
  `~/.config/gitforgery2/config.json`.

## Build and run

```sh
make build
./bin/gitforgery [repo]   # repo defaults to the current directory

make install              # installs the `gitforgery` binary into $(go env GOBIN)
```

The binary is always named `gitforgery` (including in the release archives
published by GoReleaser).

Flow: select harness → confirm the commit range → review the generated
instruction → run the harness → review/edit patches → accept → confirm
force-push.

## Keys

`Ctrl+C` quits at any time. Review mode has a view mode and an edit mode; the
selected patch is shown in the diff pane.

### Setup

| Key | Action |
| --- | --- |
| `up` / `k` | Move selection up |
| `down` / `j` | Move selection down |
| `enter` | Use the selected harness and save the choice to config |

### Range

| Key | Action |
| --- | --- |
| typing | Edit the range (e.g. `HEAD~3..HEAD`) |
| `enter` | Accept the range; an empty range uses the repo default |

The range must be linear (no merge commits) and must end at `HEAD`. Merge
base syntax such as `A...B` is rejected.

### Generate instruction

| Key | Action |
| --- | --- |
| `esc` | Skip instruction generation and go to the instruction screen |

Instruction generation is capped by the same 3-minute timeout as a run.

### Instruction

| Key | Action |
| --- | --- |
| typing | Edit the instruction (multi-line) |
| `enter` | Insert a newline |
| `tab` | Toggle "Don't modify final state" (see [Safety](#safety)) |
| `ctrl+s` / `ctrl+enter` | Accept the instruction and run the harness |
| `esc` | Return to the Range screen |

An empty instruction defaults to `improve commit messages`.

### Run

The harness streams its output while it runs.

| Key | Action |
| --- | --- |
| `esc` | Cancel the run (kills the harness child and keeps the temp patch directory) |
| `ctrl+c` | Quit the TUI |

A run is also capped by a 3-minute timeout, as are instruction generation and
reprompts.

### Review (view mode)

| Key | Action |
| --- | --- |
| `up` / `k` | Select previous patch |
| `down` / `j` | Select next patch |
| `pgup` / `pgdown` | Scroll the diff a page |
| `ctrl+u` / `ctrl+d` | Scroll the diff half a page |
| `home` / `end` | Jump to the top / bottom of the diff |
| `e` | Edit the selected patch |
| `r` | Reprompt the harness with a revised instruction |
| `a` / `enter` | Accept the patches and go to confirmation |
| `q` / `esc` | Abort and discard the temp patch directory |

### Review (edit mode)

| Key | Action |
| --- | --- |
| `ctrl+s` | Save the patch and return to view mode |
| `esc` | Discard changes and return to view mode |
| typing | Edit the patch in the textarea |

### Confirm force-push

| Key | Action |
| --- | --- |
| `y` / `enter` | Force-push the rewritten history |
| `n` / `q` / `esc` | Abort before pushing |

### Done

| Key | Action |
| --- | --- |
| `enter` / `q` | Exit |

## Safety

- The pre-rewrite tip is saved as `refs/gitforgery2/original`. This backup ref
  is write-once per original tip: it is created if absent, but an existing
  backup ref is never overwritten. To rewrite again from a new tip, move or
  delete the old backup ref first (e.g. `git update-ref -d
  refs/gitforgery2/original`).
- "Don't modify final state" is **OFF by default**; press `tab` on the
  instruction screen to turn it on. This is the opt-in guarantee from RFC §5.
  With it enabled, all patches are applied to a temporary index built from the
  range base and the resulting tree OID must match the snapshot; otherwise the
  run aborts, prints a diff summary, and no force-push happens.
- On a final-state mismatch or a harness failure, the temporary patch directory
  is kept and its path is printed on the error screen, so the generated patches
  can be inspected or reused.
- The harness prompt is the only thing sent to the harness; no API keys are
  stored by this tool.

## Recovery

If a run aborts or a push fails, the original tip is still available through
the backup ref. Restore the branch exactly as it was:

```sh
git reset --hard refs/gitforgery2/original
```

The TUI reuses the harness choice persisted at
`~/.config/gitforgery2/config.json`. To make it ask again (for example after
installing or removing `pi`, `codex`, or `claude`), reset that file:

```sh
rm ~/.config/gitforgery2/config.json
```

## Test

```sh
make test        # unit + integration (real temp git repos, fake harness)
make test-race
```
