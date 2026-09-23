# Workspace continuity and execution preview

**Status:** shipped (P1 workspace and Preview), 2026-09-23; debugger remains P? / L
**Effort:** L
**Related:** [TODO](../TODO.md), [variable provenance and graphs](variable-provenance-graphs.md), `internal/ui/app.go`, `internal/ui/run_review.go`, `internal/ansible/run.go`

Interactive step/debug support is a separate **P? / L** investigation within this
note. The workspace and read-only Preview implementation landed in the local source
trial. Interactive debugging remains deferred.

## Implementation record

The implementation retains a Logs / Roles / Preview workspace and a shared
playbook request. Tags use an explicit draft, merge local and native observations,
and remember applied selections per playbook for the current session. `p` is an
explicit refresh, not an automatic consequence of moving or applying tags.

Roles defaults to direct declarations in the selected file, with repeated uses
and unresolved imports/includes retained. `a` opens all project roles, `f` opens
source selection and preview, and `s` suggests observed declaration tags. `r`
reviews the current playbook. The standalone role action is named explicitly in
Actions and does not inherit playbook tags or parent execution context.

Native scope observation uses the same prepared command context for
`--list-hosts --list-tasks --list-tags`. CLI equivalents are `preview PLAYBOOK`
and `inspect tags PLAYBOOK`; structured rows retain native output as the fallback.
The implementation does not provide complete variable provenance, a dependency
graph, automatic chezmoi policy reconstruction, task stepping or debugger stdin.

Parser/model tests cover source identity, repeated/unresolved role references,
draft cancellation, stale observations, native request equivalence, source
refresh and small-screen rendering. PTY verification uses disposable fake
Ansible processes; the final implementation report records executed checks.
The original rationale and debugger investigation are retained below.

## Why this surfaced

The user's trial screenshots and feedback exposed a break in the main workflow.
The Tags screen reads as a separate list instead of a small adjustment to the
selected playbook. Returning from selection does not make the effect on the
pending run immediately obvious. The Role Browser likewise feels disconnected:
the user cannot readily tell how its selected role relates to the playbook they
were examining, or whether choosing it is inspecting content or changing what
will run.

The Run review contains useful information, but presents it as a plain block of
text with weak visual hierarchy. The user wants a coherent path from choosing a
playbook, through narrowing work, to seeing the expected steps and actual result.
No screenshots, private paths, inventory contents, or credentials are copied into
this note.

Source inspection agrees with the distinction between working mechanics and
unfinished continuity: tag confirmation updates the playbook selection and a
short status message; roles are independently scanned from the project roles
directory; shared run plans already carry cwd, runtime and flags, while review
formats those fields into one scrolling text body. Build on these shared
operations rather than adding another execution path.

## Proposed P1 interaction

- Keep the project, selected playbook, inventory/limit and run mode visible while
  changing a choice. Preserve pane focus, selected identity, filter and scroll
  when entering and leaving temporary controls; retain a compact context header
  on narrow terminals.
- Make Tags a compact picker associated with the selected playbook. Space toggles
  a draft selection, Enter applies it, and Esc cancels it. Returning should show
  the applied tags beside that playbook and in the pending run summary. Clearing
  the selection must visibly restore the unfiltered tag state.
- Make role inspection explain its relationship to the selected playbook when
  known, with source navigation and an explicit unknown/dynamic state otherwise.
  Enter remains inspection. An execution action must say whether it runs the
  selected playbook with its context or creates a standalone role play.
- Do not infer a tag from a role's name or label a standalone role run as a slice
  of its parent playbook. A standalone run does not automatically reproduce the
  parent's play variables, earlier tasks, conditions or handler context. Offer
  playbook tag filtering only from observed tags and show its effect in preview.
- Give shared Run review a clear target/context area, selected flags, a redacted
  command, and distinct Preview, Check/Diff and Execute actions. Keep Cancel as
  the initial confirmation selection. Use the same visual vocabulary for
  preparing, empty results, errors, running and completed results; keep raw logs
  available for details.

This is a workspace improvement, not a commitment to an IDE, custom Ansible
parser or dependency graph. Keep structure/provenance investigation in the
[existing graph note](variable-provenance-graphs.md).

## First slice: observe the selected execution scope

Start with an explicit **read-only Preview** action for the selected playbook and
an equivalent scriptable entry point. Reuse the shared run request to invoke
`ansible-playbook --list-hosts`, `--list-tasks` and `--list-tags` with the same
cwd, selected runtime, inventory, playbook, tags, limit and extra-variable inputs.
These commands list information without executing playbook tasks; they still
load the configured inventory and plugins. Keep command-plan-only `--dry-run`
distinct from this observation and from Ansible check mode. [CLI reference](https://docs.ansible.com/projects/ansible/latest/cli/ansible-playbook.html)

Show matched hosts, available tags and observed task order with the request
summary. Retain actual command output and diagnostics; a parser for structured
presentation needs fixtures for supported Ansible output, with an honest raw
output fallback. A host match does not prove that a task will run on it after
conditions and filtering.

Run discovery asynchronously with timeout and cancellation. Tie each response
to the exact selection generation; changing cwd/runtime, playbook, inventory,
limit, tags or variable inputs invalidates the old preview. Preserve a useful
previous result while marking it stale or loading. An empty match, unavailable
result and failed command are different states. Keep sensitive inputs in memory
for the active operation and redact previews, errors and persisted data.

Static lists do not fully expand dynamic `include_tasks` / `include_role`
children. Imports are processed earlier and can be listed; includes may depend
on conditions or earlier results. Display this boundary alongside the list,
and do not invent missing task branches or promise final task counts. [Reuse comparison](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_reuse.html)

After the read-only slice is useful, an explicit **Check/Diff** action can reuse
the same reviewed request. It must go through execution confirmation: supported
modules simulate changes, unsupported tasks may provide no prediction, and a
task with `check_mode: false` can execute normally. Conditions depending on
registered outputs can also make a check incomplete. Diff output may disclose
sensitive file content. Do not trigger check mode automatically when opening
preview or changing a tag. [Check and diff modes](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_checkmode.html)

## Native capabilities and their boundaries

| Capability | Useful result | Boundary for the product |
| --- | --- | --- |
| `--syntax-check` | Parse/validate without running playbook tasks | A passing check does not validate all runtime values or remote behavior. |
| `--list-hosts`, `--list-tasks`, `--list-tags` | Observe selected hosts and statically available tasks/tags | Dynamic included children and runtime branches can remain unknown. |
| `--check --diff` | Run supported change prediction and diff reporting | An execution mode, not an unconditional guarantee of no changes. |
| `--step` | Before each task: `y` runs, `n` skips, `c` continues without further prompts | Confirmed tasks do actual work unless their applicable check mode says otherwise. No durable checkpoint/resume semantics. |
| `--start-at-task NAME` | Start at a named task | Cannot target dynamic included children; play fact gathering or argument validation may still run first. |
| Task debugger | Inspect task state, change variables/arguments, retry or continue | Operates in the current execution; retry can run the task again. No stored replay of all preceding state. |

Listing and syntax options are in the
[CLI reference](https://docs.ansible.com/projects/ansible/latest/cli/ansible-playbook.html).
Step responses, start-at-task limits and initial implicit tasks are documented in
[execution troubleshooting](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_startnstep.html).
The simulation boundary is documented in
[check/diff mode](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_checkmode.html).

Partial execution also needs a context warning derived from Ansible's variable
model: registered results live in memory for the current run, and tag-filtered
tasks do not create their normal registered result. Starting a new run later in
the playbook cannot be assumed to reconstruct earlier registered values,
uncached facts, `set_fact` results, setup changes or handler notifications.
Fact gathering or an existing fact cache can supply some data, but neither is a
general replay of preceding tasks. This is an inference about partial execution,
not a claim that every skipped task leaves every variable undefined. [Registered-variable lifetime](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_variables.html#registering-variables)

## Interactive step debugging

**Status:** P? / L — research before choosing a UI implementation.

Ansible's debugger is disabled by default. A play/task can use
`debugger: on_failed`; global activation also exists through
`ANSIBLE_ENABLE_TASK_DEBUGGER=True`. It can inspect values, edit task arguments or
variables, rebuild the task with `update_task`, and `redo`, `continue` or `quit`.
With the `free` strategy, already queued tasks can execute before a retried task.
Strategy and host concurrency therefore affect the interaction. [Official task debugger](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_debugger.html)

The first spike should compare a deliberate terminal handoff to native
`--step`/debugger against an embedded interactive session. The current execution
service streams output while Bubble Tea owns keyboard input; simply appending
`--step` cannot provide dependable interactive control. Establish one terminal
reader, restore terminal modes and workspace context on return, and define
cancel/abort behavior before exposing a button. Do not guess debugger prompts
from arbitrary stdout or claim cancellation rolls back completed remote work.

Use disposable fixtures for static imports, dynamic includes, duplicate task
names, registered results, uncached facts, ignored failures and multiple hosts.
The spike must decide whether native handoff already meets the user's need,
how skipped prerequisite context is communicated, and whether an embedded UI
justifies owning stdin, prompts, retries and interrupted sessions. No interactive
debug implementation is part of the P1 read-only preview slice.

## Acceptance for the next implementation round

- Select a playbook, open Tags, filter and select with arrows/Vim keys, then apply.
  The same playbook/focus/scroll remains selected, applied tags are visible, and
  the next preview receives those exact tags. Cancel keeps the old selection;
  printable input keys never invoke global actions.
- Open a related role, inspect its source, and return without losing context.
  Any run action distinguishes the existing playbook context from a standalone
  role play; a role name never silently becomes a tag filter.
- For a static-import fixture, preview reflects selected hosts, task order and
  tags. For dynamic includes, identify the observation limit instead of showing
  an invented complete expansion.
- Fake processes verify identical runtime/cwd/inventory/limit/tags/vars across
  CLI and TUI previews, no execution command during read-only preview, bounded
  errors/cancellation, secret redaction and rejection of stale responses.
- Exercise resize and terminal input in a PTY, including tiny widths, Chinese
  names, slow discovery, empty matches, Escape and return from overlays. Keep
  optional check/diff execution separate and test it only on a disposable fixture.

**Original decision, 2026-09-23:** record workspace continuity and read-only execution
preview as P1/L. Keep step/partial-execution debugging
at P?/L until the terminal and execution-context spike is complete. No actual
playbook was run during this investigation.
