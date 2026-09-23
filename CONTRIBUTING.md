# Developing this fork

This is the permanent personal fork `daviddwlee84/lazyansible`. Changes here do
not imply a contribution to `kocierik/lazyansible`. Preserve its MIT attribution.
Read [AGENTS.md](AGENTS.md) before modifying the project.

## Build and verify

Use the Go version declared in [go.mod](go.mod), currently Go 1.24.2 or newer.
The existing Bubble Tea v1, Bubbles, and Lip Gloss versions remain the UI stack.

```sh
mkdir -p bin
go build -o bin/lazyansible ./cmd/lazyansible
go test ./...
go vet ./...
python3 testdata/pty_smoke.py ./bin/lazyansible
```

Run `go test -race ./...` after concurrency changes. Use `gofmt` on changed Go
files. Tests use isolated HOME/XDG directories, fake executables, or the disposable
localhost fixture under `testdata/localhost`. Never use a real inventory or a full
dotfiles apply as a routine smoke test. The PTY harness checks actual input and
terminal restoration; model tests alone cannot establish those properties.

## Code boundaries

- `internal/ansible` owns executable resolution, observations, command plans,
  and shell-free process execution. Both CLI and TUI call it.
- `internal/cli` handles arguments, output formats, and review/confirmation.
- `internal/ui` owns interaction state; blocking subprocess work belongs in
  Bubble Tea commands. Overlay state does not own process completion.
- `internal/config` and `internal/paths` own preferences and XDG policy.
  Profiles belong in config, history in state, update observations in cache.
- `internal/buildinfo` supplies one CLI/TUI version. Local checkout builds are
  `dev`; an eventual release can inject `Version`, `Commit`, and `Date`.

Text input owns printable keys. Navigation supports arrows and Vim aliases.
Keep action hints and dispatch consistent. Preserve filters, selection identity,
and project context across refresh and external-editor handoff.

## Review and reports

Include the observable behavior, motivation, relevant checks, and remaining
limits. Keep the [Unreleased changelog](CHANGELOG.md) current. Agent involvement
must not be inferred from style; describe actual assistance when publishing work.
Preserve unrelated live `.specstory` files and keep private configuration and
transcripts out of publication.

Use [TODO.md](TODO.md) for deferred work and its linked research notes to preserve
decisions. Capture recurring traps in [pitfalls/](pitfalls/README.md).

## Distribution boundary

The current workflow is local source build and rebuild, not a supported packaged
release. The manual snapshot workflow builds artifacts only. It does not publish
on tags or update any package repository. `.goreleaser.yml` names this fork and
contains no upstream tap/bucket targets. The inherited AUR package was removed
because it downloaded upstream binaries.

Before a first release, complete the [distribution follow-up](backlog/release-and-upgrade.md):
choose supported channels, test upgrade ownership and the effective installed
copy, and verify an immutable public revision. Do not run an undocumented release
helper, push, tag, or publish merely because a local build passes.
