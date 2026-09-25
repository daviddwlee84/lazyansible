# Project guidance

This is the permanent personal fork `daviddwlee84/lazyansible`, derived from
`kocierik/lazyansible` under MIT. Preserve upstream attribution. Work here does
not imply an upstream contribution or permission to publish a release.

## Implementation contracts

- Retain the compatible Bubble Tea v1 stack. Keep views and input handlers thin;
  CLI and TUI operations share domain services and runtime resolution.
- Use XDG paths on macOS and Linux: preferences/profiles in config, history in
  state, rebuildable update observations in cache. Reads do not create files.
- Resolve the actual Ansible executable and its owner. Updates must target that
  uv tool, preserve its installation settings, and never upgrade all tools.
- Process background results regardless of the visible overlay. Use request
  identity to reject stale results. No subprocess/network work in View.
- Focused text input owns printable keys. Support arrows and Vim navigation;
  preserve selection by identity and restore context after overlays/editors.
- All runs have an explicit project cwd, a reviewable command plan, and one
  execution path. Never evaluate command strings through a shell.
- Keep secrets out of command previews and diagnostic output. Inventory/config
  observations are not a complete account of runtime variables or provenance.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`, and `go build ./cmd/lazyansible`.
Use race tests for concurrent changes and a real PTY for input/lifecycle changes.
Tests use temporary HOME/XDG state, fake processes, or a disposable localhost
playbook. Never apply a real inventory or the user's dotfiles as a smoke test.
Report unverified platforms and failing checks honestly.

## Working tree and delivery

Preserve unrelated user changes, especially live `.specstory` artifacts. Keep
private configuration and transcripts out of publication. The fork publishes tested source and binary releases; see docs/distribution.md. Do not
advertise upstream package-manager channels as installers for this fork.

<!-- project-knowledge-harness:agent-guidance -->
## Project memory

Use the `project-knowledge-harness` skill for long-term capture. `TODO.md` is the
single future-work index: P1/P2/P3/P? priority lanes and S/M/L/XL effort tags.
Use the skill's `add-todo.sh` and `promote-todo.sh` scripts (from the installed
skill, not an assumed repository `scripts/` directory), then validate its format.

- `backlog/` stores resume-friendly evidence and options for P?, L/XL, or paused investigations. Link each note from TODO.
- `backlog/inbox.md` holds ideas whose scope or priority is not yet known.
- `pitfalls/` records resolved traps by symptom, with exact error text and the tested fix; link normal usage caveats instead of duplicating them.
- Current behavior belongs in README/CONTRIBUTING; do not create parallel ROADMAP/IDEAS/LESSONS files.

These notes stay in source and are not binary runtime assets. Preserve them when
work ships, mark their status, and review for private data before publication.
<!-- project-knowledge-harness:agent-guidance --> (end)

## Binary distribution

See `docs/distribution.md`. Run GoReleaser config/snapshot checks and
`scripts/check-distribution.py` before tagging. Preserve immutable releases and
source/module exclusions. Backend setup is separate from installing this CLI.
