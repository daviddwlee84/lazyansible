# TODO

Deferred work for this personal fork. Priority and effort are independent;
`P?` means evaluate before committing to implementation. Current behavior is
documented in [README.md](README.md), not this index.

## P1

- [ ] **[L] Keep tag, role and execution views in one workspace** — Preserve selected project/playbook context, make tag application and role actions explicit, and add task/host/tag previews before a styled shared run review and result. → [research](backlog/workspace-execution-ux.md)

## P2

- [ ] **[L] Prepare a supported release and upgrade path** — After the local trial, choose supported platforms, publish a fork-owned source/binary release, and verify an existing installation upgrades through its owner. → [research](backlog/release-and-upgrade.md)

## P3

No additional deferred work selected.

## P?

- [ ] **[?/L] Evaluate variable provenance and dependency graphs** — Separate observable inventory/config origins from execution-dependent precedence before choosing a graph UI. → [research](backlog/variable-provenance-graphs.md)
- [ ] **[?/L] Evaluate partial execution and interactive task debugging** — Compare native step/start-at-task and failure-debugger handoff against an embedded session; establish context, cancellation and terminal-ownership limits before implementation. → [research](backlog/workspace-execution-ux.md#interactive-step-debugging)

## Done

Completed current-session work belongs in [CHANGELOG.md](CHANGELOG.md). Promote
future entries here with their date and original priority/effort when implemented.
