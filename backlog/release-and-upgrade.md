# Supported release and upgrade path

**Status:** P2 — deferred until the local UX trial is accepted
**Effort:** L
**Related:** [TODO](../TODO.md), [CONTRIBUTING](../CONTRIBUTING.md), `.goreleaser.yml`

## Current decision

2026-09-23: build and rebuild the local checkout. No release, tag, package-manager
repository, or self-update command is required to evaluate the experience.
Ansible's shared uv lifecycle is implemented separately and must remain separate.

The permanent fork is `daviddwlee84/lazyansible`, with module path
`github.com/daviddwlee84/lazyansible` and main package `cmd/lazyansible`. Original
MIT attribution remains. The inherited Homebrew/Scoop publisher destinations and
AUR file downloaded or published upstream artifacts; they were removed. The
manual workflow now builds snapshots with read-only repository permissions.

## Choices to settle later

| Channel | Benefit | Required verification |
| --- | --- | --- |
| Go module install | Smallest supported source channel | Fixed tag and `@latest` from a clean module cache; actual main-package path |
| Binary releases | No Go toolchain required on target | macOS/Linux architectures, checksums, archive contents, version identity |
| Personal Homebrew tap | Ownership and upgrades fit the maintainer's existing tools | Formula source, installed-path ownership, actual `brew upgrade` behavior |

Select only the channels needed by real trial users. Do not create a downloader,
Scoop bucket, AUR package, or fleet installer by analogy alone.

## Acceptance before publishing

1. Freeze a reviewed immutable revision; tests, PTY checks, docs, and build metadata agree.
2. Pick the first fork version and release notes. Local checkout builds stay `dev`; module installs recover their published version; release flags inject Version/Commit/Date.
3. Verify fresh installation **and** an existing installation's path to the next version. A future `upgrade --check` is read-only; `upgrade` delegates to the verified owner and preserves local/manual builds.
4. Verify source/module/archive boundaries; exclude private local artifacts without removing required fixtures, licenses, or embedded resources.
5. After authorized publication, update the catalog's public snapshot and evidence. A successful local build is not evidence of a published fork release.

## Resume point

First collect the local trial feedback. The next decision is supported channels,
not implementation of a generic updater. Publishing remains a separate explicit
operation after the artifacts above are reviewable.
