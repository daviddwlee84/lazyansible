# lazyansible

A keyboard-driven Ansible workspace: inspect inventory, choose a playbook, review
the command, and follow the result in the same terminal.

This is the permanent personal fork
[`daviddwlee84/lazyansible`](https://github.com/daviddwlee84/lazyansible), derived
from [`kocierik/lazyansible`](https://github.com/kocierik/lazyansible) under MIT.
The current fork changes are an early local trial on macOS and Linux. Build this
checkout to try them; the upstream Homebrew, Scoop, and AUR packages install the
upstream project, not these changes.

## Start here / 使用指南

- [Ansible 原生工作流程](docs/ansible-basics.zh-TW.md): inventory、playbook、role、tag 的關係，設定從哪裡來，以及這台電腦的 chezmoi / `~/.ansible` 配置。
- [lazyansible 操作指南](docs/lazyansible-guide.zh-TW.md): 逐步練習 role/tag 導覽、執行確認、CLI 對照和目前限制。

For role navigation, press uppercase `O`. For tags, focus Playbooks with `2`,
select a playbook, then press `t`. Enter inspects; `r` opens a run review.
The guides start with a debug-only project that actually contains a role and tags.

## Try this checkout

Requires **Go 1.24.2+** to build. An existing Ansible installation is usable;
**uv** is needed only for the optional shared-tool installation and upgrade
commands. `ansible-lint`, an external editor, and desktop notification utilities
are optional integrations.

```sh
# From this checkout
mkdir -p bin
go build -o bin/lazyansible ./cmd/lazyansible
./bin/lazyansible --version
./bin/lazyansible --help

# Select a project explicitly; relative inventory paths use that directory.
./bin/lazyansible -C /path/to/ansible -i inventories/localhost.ini -d playbooks
```

`-C` / `--chdir` chooses the process working directory (`--workdir` is an alias).
A bare invocation opens the dashboard in a terminal. Noninteractive usage uses
the CLI subcommands; it never opens a prompt implicitly.

A localhost tutorial project is included for learning the interface:

```sh
./bin/lazyansible -C testdata/tutorial -i inventory.ini -d .
```

Select `site`, press `t` to choose `greeting` or `summary`, and press uppercase
`O` to inspect the `demo` role. All authored tasks print debug messages; they do
not install packages or change system configuration. Press `r` on the playbook
to review; the review initially selects Cancel. Follow the
[step-by-step walkthrough](docs/lazyansible-guide.zh-TW.md) for the exact keys.

`testdata/localhost` remains a separate regression fixture for changed, skipped,
and ignored-failure output. It intentionally has no roles or tags.

To update this trial binary, update the checkout and run the same build command.
There is deliberately no lazyansible self-updater yet. Ansible updates below are
a separate operation. See the [release follow-up](backlog/release-and-upgrade.md).

## Everyday interaction

The main screen keeps inventory, playbooks, host status, and logs together.
`:`, the action palette, exposes configuration inspection, resolved inventory,
Ansible runtime management, and lazyansible settings without memorizing keys.
`?` shows actions applicable to the focused panel.

| Context | Keys | Action |
| --- | --- | --- |
| Navigation | Arrows or `j` / `k`; `g` / `gg`, `G` | Move, first, last |
| Focus | Tab / Shift+Tab, `1`–`4` | Switch panels |
| Inventory | `h` / `l`, Left / Right, Space | Collapse, expand, toggle groups |
| Inventory | Enter; `s` | Inspect selection; set host/group limit |
| Playbooks | Enter / Space; `r` | View source; review a run |
| Playbooks | `c`, `d`, `t`, `e` | Check mode, diff mode, tags, extra variables |
| Lists and logs | `/`; Esc | Search/filter; leave input or go back |
| Logs | `n` / `N`, `G`, Ctrl+D / Ctrl+U | Search matches, follow bottom, half-page scroll |
| Anywhere outside a field | `:`, `?`, `q` | Actions, help, quit |

Text fields own printable keys: typing `j`, `q`, or `/` does not navigate or quit.
Outside the inventory tree, `h` / `l` also changes panel focus. History (`H`), run
profiles (`F`), SSH profiles (`P`), role browsing (`O`), ad-hoc commands (`!`),
Vault (`V`), Galaxy (`A`), and inventory switching remain available through help
and the action palette.

Before a playbook, ad-hoc, or role execution, review shows the project directory,
inventory/target, active Ansible runtime, flags, and a redacted command. The CLI
and TUI use the same preparation and execution service. Background observations
and running commands leave navigation available; Esc returns from an inspector.

## Preferences and storage

macOS and Linux use XDG locations consistently:

| Content | Location | Default |
| --- | --- | --- |
| Preferences | `$XDG_CONFIG_HOME/lazyansible/config.yml` | `~/.config/lazyansible/config.yml` |
| Run and SSH profiles | Same config directory, `*-profiles.json` | `~/.config/lazyansible/` |
| Run history | `$XDG_STATE_HOME/lazyansible/history/` | `~/.local/state/lazyansible/history/` |
| Update observations | `$XDG_CACHE_HOME/lazyansible/` | `~/.cache/lazyansible/` |

Relative XDG variables are ignored. Reads do not create these directories; saves
create private files. The selected config is `--config`, then
`LAZYANSIBLE_CONFIG`, then the XDG default. If the default does not exist,
`~/.lazyansible/config.yml` remains a read fallback. An explicit missing or
invalid config reports an error. No project-local preferences are loaded
implicitly.

```sh
./bin/lazyansible config init       # exclusive create; never overwrites
./bin/lazyansible config show       # effective values and selected path
./bin/lazyansible config show --json
./bin/lazyansible config edit       # VISUAL, then EDITOR; quoted arguments work
```

`--init-config` remains an alias for `config init`. `config edit` opens the selected
file even when it is malformed and validates saved edits afterward. Restart the
dashboard to apply changed preferences.

```yaml
# ~/.config/lazyansible/config.yml
# Relative paths use the launch working directory.
# inventory: inventories/localhost.ini
# playbook_dir: playbooks
no_mouse: false
notify_on_finish: false
default_check_mode: false
default_diff_mode: false
check_updates: true
# Optional runtime selection; existing uv receipts own version/Python policy.
# runtime:
#   executable: /path/to/ansible-playbook
#   uv_executable: /path/to/uv
#   package: ansible-core
```

Explicit flags override file values, including `--no-mouse=false`, `--check=false`,
`--diff=false`, `--notify=false`, and `--check-updates=false`.

Profiles read their legacy `~/.lazyansible/*-profiles.json` file until an XDG
replacement exists; future profile saves use XDG. History merges both locations,
with XDG records winning duplicate IDs. Legacy files are not deleted or rewritten.
History omits sensitive run inputs. A record requiring omitted arguments, extra
variables, or Vault input cannot be replayed silently; configure the run again.
Legacy history without a recorded working directory remains viewable but cannot
be replayed by guessing a project.

## Shared Ansible runtime

Runtime status resolves the active executable and verifies whether it belongs to
an existing **uv tool** environment. Playbook, inventory, configuration, and
Galaxy companion commands come from that installation. An ordinary launch never
installs or upgrades Ansible.

```sh
./bin/lazyansible runtime status --json
./bin/lazyansible runtime check --json

# Review first; execute explicitly when ready.
./bin/lazyansible runtime upgrade --dry-run
./bin/lazyansible runtime upgrade --yes

# For a new installation, optionally choose a version/Python request.
./bin/lazyansible runtime install ansible-core --python 3.13 --dry-run
./bin/lazyansible runtime install ansible-core --python 3.13 --yes
```

Upgrades target only the detected package, for example `uv tool upgrade
ansible-core`, preserving the existing uv tool's constraints and settings. They
do not upgrade uv, every Python tool, Ansible collections, or lazyansible. A
non-uv installation reports its ownership instead of being overwritten.

Update observations respect the installed tool's indexes and Python. They are
cached for 24 hours; an explicit `runtime check` refreshes the observation.
Network/unsupported-policy failures are shown as unknown rather than current.
A newer release may be outside an installed version constraint, so an available
update does not promise the next upgrade will choose that exact version.
Disable automatic background checks with `check_updates: false`; manual checking
and runtime status remain available. In the Runtime view, `c` checks, `u` reviews
an upgrade, and `i` reviews installation.

## Inspect and automate

```sh
./bin/lazyansible -C /path/to/ansible inspect inventory --json
./bin/lazyansible -C /path/to/ansible inspect config --json
./bin/lazyansible -C /path/to/ansible run playbooks/site.yml --check --diff --dry-run
./bin/lazyansible -C /path/to/ansible adhoc all -m ansible.builtin.ping --dry-run
./bin/lazyansible -C /path/to/ansible role run roles/example --hosts staging --dry-run
```

Execution commands print the plan and request one confirmation in a terminal.
Use `--yes` for explicit noninteractive execution. `--dry-run` prepares the same
command without executing it; `--json` supports observations and dry-run plans,
not execution. Diagnostics go to stderr. Child exit status is preserved, usage
errors return 2, and cancellation returns 130.

Inventory inspection uses `ansible-inventory --list`; configuration inspection
uses changed settings and origins reported by `ansible-config`. These observations
are **not complete task-time variables or variable provenance**. Facts, dynamic
includes, role/task context, and extra variables can change execution results.
Sensitive keys and command arguments are redacted, but arbitrary output from an
Ansible task can still contain whatever that task prints.

For parsed execution output, lazyansible sets `ANSIBLE_STDOUT_CALLBACK=default`
and disables color **only in its child processes**. This leaves an existing
`ansible.cfg`, including a custom `clean` callback used by dotfiles, unchanged.
The review and config inspector disclose these execution overrides.

## Development and future work

```sh
go test ./...
go vet ./...
go build -o bin/lazyansible ./cmd/lazyansible
python3 testdata/pty_smoke.py ./bin/lazyansible
python3 testdata/pty_smoke.py ./bin/lazyansible --signal
```

The PTY harness uses temporary HOME/XDG directories and fake Ansible programs;
it needs Python 3 and no external Python package. See [CONTRIBUTING.md](CONTRIBUTING.md)
and [AGENTS.md](AGENTS.md) for architecture and verification rules.

[TODO.md](TODO.md) indexes deferred work; [backlog/](backlog/README.md) retains
research and [pitfalls/](pitfalls/README.md) records known traps. Full variable
provenance and graph rendering are [evaluation work](backlog/variable-provenance-graphs.md).
No ansible-navigator, execution environment, or ansible-dev-tools bundle is
required for this workflow.

## License

[MIT](LICENSE). Original copyright and attribution to kocierik are preserved.
