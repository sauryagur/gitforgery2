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
```

Flow: select harness → confirm the commit range → review the generated
instruction → run the harness → review/edit patches → accept → confirm
force-push.

## Safety

- The pre-rewrite tip is saved as `refs/gitforgery2/original`.
- With "Don't modify final state" enabled, all patches are applied to a
  temporary index built from the range base and the resulting tree OID must
  match the snapshot; otherwise the run aborts, keeps the temp patch directory,
  and prints a diff summary. No force-push happens.
- The harness prompt is the only thing sent to the harness; no API keys are
  stored by this tool.

## Test

```sh
make test        # unit + integration (real temp git repos, fake harness)
make test-race
```
