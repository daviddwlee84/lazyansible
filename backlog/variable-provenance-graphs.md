# Variable provenance and dependency graphs

**Status:** P? — evaluation, deferred 2026-09-23
**Effort:** L
**Related:** [TODO](../TODO.md), `internal/ansible/inspect.go`, `internal/ui/workbench.go`

## Motivation and current boundary

The desired experience is to understand which inventory/group/role/settings feed
a selected run, where a value was introduced, and what a `when`, include, or
handler means without learning another large toolchain.

The current inspector intentionally stops at two useful observations:

- `ansible-inventory --list` provides inventory-resolved hosts, groups, and variables.
- `ansible-config dump --only-changed` provides changed configuration values and reported origins.

These do not establish a complete value history or task-time effective variables.
The current run review shows cwd, target, flags, runtime and a redacted command;
it does not predict every branch or remote change.

## Evidence reviewed

Reviewed the official [precedence documentation](https://docs.ansible.com/projects/ansible/latest/reference_appendices/general_precedence.html)
on 2026-09-23. Configuration, command options, playbook keywords, variables, and
direct assignment are distinct precedence categories. A configuration origin is
therefore not a provenance graph for a task variable. Facts, extra variables,
role/task scope and execution results need their own evidence.

Reviewed [ansible-playbook-grapher](https://github.com/haidaraM/ansible-playbook-grapher)
on 2026-09-23. Its documented JSON renderer and source locations are a plausible
input for a structural browser; Graphviz is an optional rendering dependency.
Its isolated Python environment avoids coupling the graph adapter's Ansible
requirements to the user's execution runtime. No package was installed and no
representative dynamic playbook was evaluated in this review.

## Options and tradeoffs

| Option | Useful outcome | Uncertainty / cost |
| --- | --- | --- |
| Extend present inspectors with source links | Low learning cost; exact observed config origins and editable sources | Cannot explain every variable override |
| Optional grapher JSON adapter | Play/role/task/include structure with source navigation | Versioned external schema; static output cannot establish all runtime branches |
| Capture execution events and scoped values | Explain what actually happened for a particular host/run | Instrumentation, secret handling, storage cost, and incomplete historical evidence |

## Next spike

Use a disposable fixture containing group/host vars, role defaults/vars,
`set_fact`, registered values, extra vars, conditional dynamic includes, and
handlers. Compare actual execution evidence with inventory/config/grapher output.
Document which edges are statically observed, runtime-observed, conditional, or
unknown; never render an inferred edge as certainty.

The spike must answer which questions a source-linked tree already solves,
whether a graph improves navigation, and what evidence a value explanation needs.
Keep graph work optional and isolated from the shared Ansible uv tool. Do not
implement an independent Ansible parser or install ansible-navigator/dev-tools
merely to render the first inspector.
