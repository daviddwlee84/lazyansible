# Roles and tags tutorial fixture

This project contains one localhost inventory, one role (`demo`), and two
playbook tags (`greeting`, `summary`). Its authored tasks only print debug
messages. It does not install packages or change system configuration.

From the lazyansible repository root:

```sh
./bin/lazyansible -C testdata/tutorial -i inventory.ini -d .
```

Select `site`, open `t` to apply `greeting`, and press `p` to observe the selected
hosts/tasks/tags. The role task should be listed and the `summary` task filtered
out. `r` reviews this same playbook before execution.

`O` opens its related role declarations. `a` switches to all project roles,
`f` browses source files, and `s` suggests the declaration's `greeting` tag in the
normal draft picker. `r` in Roles still reviews `site.yml`. To compare independent
role defaults with play variables, use the explicit **Review standalone role**
Actions entry; its new play does not inherit parent tags or variables.

```sh
./bin/lazyansible -C testdata/tutorial -i inventory.ini \
  preview site.yml --tags greeting --json
```

Follow the [native Ansible guide](../../docs/ansible-basics.zh-TW.md) and
[lazyansible walkthrough](../../docs/lazyansible-guide.zh-TW.md).
