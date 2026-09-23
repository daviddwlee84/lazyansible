# Roles and tags tutorial fixture

This project contains one localhost inventory, one role (`demo`), and two
playbook tags (`greeting`, `summary`). Its authored tasks only print debug
messages. It does not install packages or change system configuration.

From the lazyansible repository root:

```sh
./bin/lazyansible -C testdata/tutorial -i inventory.ini -d .
```

Follow the [native Ansible guide](../../docs/ansible-basics.zh-TW.md) and
[lazyansible walkthrough](../../docs/lazyansible-guide.zh-TW.md).
