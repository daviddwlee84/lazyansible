#!/usr/bin/env python3
"""Exercise the real terminal parser with disposable state and fake Ansible.

Usage: python3 testdata/pty_smoke.py /absolute/path/to/lazyansible [--signal] [--preview-quit]
No Python dependencies, network access, or real host operations are required.
"""
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time


def main():
    binary = str(Path(sys.argv[1]).resolve())
    with tempfile.TemporaryDirectory(prefix="lazyansible-pty-") as tmp:
        root = Path(tmp)
        tools = root / "tools"
        tools.mkdir()
        project = root / "project"
        project.mkdir()
        (project / "inventory.ini").write_text("[web]\nfixture ansible_connection=local\n")
        (project / "site.yml").write_text("- name: fixture\n  hosts: web\n  gather_facts: false\n  roles:\n    - role: demo\n      tags: [greeting]\n  tasks:\n    - name: Summary fixture\n      ansible.builtin.debug:\n        msg: summary\n      tags: [summary]\n")
        role = project / "roles" / "demo"
        (role / "tasks").mkdir(parents=True)
        (role / "defaults").mkdir()
        (role / "tasks" / "main.yml").write_text("- name: Demo source task\n  ansible.builtin.debug:\n    msg: '{{ demo_message }}'\n")
        (role / "defaults" / "main.yml").write_text("demo_message: fixture-message\n")
        (root / "tmp").mkdir()
        (root / "config.yml").write_text("check_updates: false\nno_mouse: true\n")
        backend = r'''#!/usr/bin/python3
import json, os, sys, time
from pathlib import Path
name = Path(sys.argv[0]).name
if '--version' in sys.argv:
 print('ansible [core 2.21.4]')
 print('  executable location = ' + str(Path(sys.argv[0]).resolve()))
 print('  python version = 3.13.3 (' + sys.executable + ')')
elif name == 'ansible-inventory':
 print(json.dumps({'all': {'children': ['web']}, 'web': {'hosts': ['fixture']}, '_meta': {'hostvars': {'fixture': {'enabled': True, 'items': [1, 2]}}}}))
elif name == 'ansible-config':
 print(json.dumps([{'name':'DEFAULT_HOST_LIST','value':['inventory.ini'],'origin':'fixture ansible.cfg'}]))
elif name == 'ansible-playbook' and any(flag in sys.argv for flag in ('--list-hosts', '--list-tasks', '--list-tags')):
 kind = 'preview' if '--list-tasks' in sys.argv else 'catalogue'
 listing_file = Path(os.environ['LAZYANSIBLE_SMOKE_LISTINGS'])
 previous = [json.loads(line) for line in listing_file.read_text().splitlines()] if listing_file.exists() else []
 tags = sys.argv[sys.argv.index('--tags') + 1].split(',') if '--tags' in sys.argv else []
 record = {'kind': kind, 'cwd': os.getcwd(), 'argv': sys.argv[1:], 'tags': tags, 'pid': os.getpid()}
 if '--vault-password-file' in sys.argv:
  path = Path(sys.argv[sys.argv.index('--vault-password-file') + 1])
  record.update(vault_path=str(path), vault_mode=path.stat().st_mode & 0o777,
                vault_valid=path.read_text().strip() == os.environ['LAZYANSIBLE_SMOKE_VAULT'])
 with listing_file.open('a') as f:
  f.write(json.dumps(record) + '\n')
 if kind == 'preview' and Path(os.environ['LAZYANSIBLE_SMOKE_SLOW']).exists():
  time.sleep(60)
 if kind == 'preview' and Path(os.environ['LAZYANSIBLE_SMOKE_FAIL']).exists():
  print('password=PTY_ERROR_SECRET https://user:URL_SECRET@example.invalid/?token=QUERY_SECRET', file=sys.stderr)
  sys.exit(7)
 print('\nplaybook: site.yml\n')
 print('  play #1 (web): fixture\tTAGS: []')
 if kind == 'preview':
  print("    pattern: ['web']\n    hosts (1):\n      fixture\n    tasks:")
  selected = []
  for tag, task in [('greeting', 'demo : Demo source task'), ('summary', 'Summary fixture')]:
   if not tags or tag in tags:
    selected.append(tag)
    print('      ' + task + '\tTAGS: [' + tag + ']')
 else:
  selected = ['greeting', 'summary']
  # A tag disappearing from later catalogues must retain an active selection.
  if sum(item['kind'] == 'catalogue' for item in previous) < 2:
   selected.append('ghost')
 print('      TASK TAGS: [' + ', '.join(sorted(selected)) + ']')
else:
 with open(os.environ['LAZYANSIBLE_SMOKE_MARKER'], 'a') as f:
  f.write(json.dumps({'cwd': os.getcwd(), 'argv': sys.argv[1:], 'callback': os.environ.get('ANSIBLE_STDOUT_CALLBACK'), 'pid': os.getpid()}) + '\n')
 for line in ['PLAY [fixture]', 'TASK [start]', 'ok: [fixture]', 'TASK [finish]', 'changed: [fixture]', 'PLAY RECAP', 'fixture : ok=2 changed=1 unreachable=0 failed=0 skipped=0 rescued=0 ignored=0']:
  print(line, flush=True)
  time.sleep(0.16)
'''
        for name in ("ansible", "ansible-playbook", "ansible-inventory", "ansible-config", "ansible-galaxy"):
            path = tools / name
            path.write_text(backend)
            path.chmod(0o755)
        edit = tools / "fake-editor"
        edit.write_text('#!/bin/sh\nprintf "EDITOR_HANDOFF_OK\\n"\n')
        edit.chmod(0o755)
        marker = root / "runs.jsonl"
        listings = root / "listings.jsonl"
        fail_preview = root / "fail-preview"
        slow_preview = root / "slow-preview"
        env = os.environ.copy()
        for key in list(env):
            if key.startswith(("ANSIBLE_", "UV_", "LAZYANSIBLE_")):
                del env[key]
        env.update(HOME=tmp, XDG_CONFIG_HOME=str(root / "config"), XDG_STATE_HOME=str(root / "state"),
                   XDG_CACHE_HOME=str(root / "cache"), XDG_DATA_HOME=str(root / "data"),
                   PATH=str(tools) + ":/usr/bin:/bin", TERM="xterm-256color", NO_COLOR="1",
                   VISUAL=str(edit), EDITOR=str(edit), TMPDIR=str(root / "tmp"),
                   LAZYANSIBLE_SMOKE_MARKER=str(marker), LAZYANSIBLE_SMOKE_LISTINGS=str(listings),
                   LAZYANSIBLE_SMOKE_FAIL=str(fail_preview), LAZYANSIBLE_SMOKE_SLOW=str(slow_preview),
                   LAZYANSIBLE_SMOKE_VAULT="PTY_VAULT_SECRET")
        master, slave = pty.openpty()
        before = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
        proc = subprocess.Popen([binary, "--config", str(root / "config.yml"), "-C", str(project), "-i", "inventory.ini", "-d", "."],
                                stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True)
        output = bytearray()

        def read_for(seconds):
            deadline = time.monotonic() + seconds
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], min(0.05, max(0, deadline - time.monotonic())))
                if ready:
                    try:
                        block = os.read(master, 65536)
                    except OSError:
                        break
                    if not block:
                        break
                    output.extend(block)

        def send(data):
            os.write(master, data)
            read_for(0.18)
            if proc.poll() is not None:
                raise AssertionError("dashboard exited unexpectedly: " + output.decode(errors="replace")[-1200:])

        def press(*keys):
            for key in keys:
                send(key)

        def plain_output(since=0):
            text = output[since:].decode(errors="replace")
            return re.sub(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))", "", text)

        def wait_text(text, timeout=8, since=0):
            deadline = time.monotonic() + timeout
            while text not in plain_output(since):
                if time.monotonic() > deadline:
                    raise AssertionError(f"did not see {text!r}: " + output.decode(errors="replace")[-2000:])
                read_for(0.1)

        def records(path):
            if not path.exists():
                return []
            try:
                return [json.loads(line) for line in path.read_text().splitlines() if line]
            except json.JSONDecodeError:
                return []  # A writer may be between its write and final newline.

        def wait_count(path, count, kind=None):
            deadline = time.monotonic() + 8
            while True:
                rows = records(path)
                if kind is not None:
                    rows = [row for row in rows if row["kind"] == kind]
                if len(rows) >= count:
                    return rows
                if time.monotonic() > deadline:
                    raise AssertionError(f"expected {count} {kind or 'run'} records, got {rows}")
                read_for(0.1)

        def preview_count():
            return len([row for row in records(listings) if row["kind"] == "preview"])

        pending_child = None

        def assert_exit(interrupted, child_pid):
            proc.wait(timeout=5)
            read_for(0.1)
            try:
                os.kill(child_pid, 0)
            except ProcessLookupError:
                pass
            else:
                raise AssertionError("Ansible child survived dashboard exit")
            assert proc.returncode == (130 if interrupted else 0), f"exit {proc.returncode}"
            after = termios.tcgetattr(slave)
            mask = termios.ECHO | termios.ICANON | termios.ISIG
            assert after[3] & mask == before[3] & mask, "terminal input mode was not restored"
            assert b"\x1b[?1049l" in output, "alternate screen was not released"

        try:
            wait_text("Inventory")
            read_for(0.5)
            if "--preview-quit" in sys.argv[2:]:
                send(b"2")
                send(b"V")
                press(b"PTY_VAULT_SECRET", b"\r")
                slow_preview.write_text("hold the observation until cancellation")
                send(b"p")
                pending = wait_count(listings, 1, "preview")[-1]
                pending_child = pending["pid"]
                assert pending["vault_valid"] and pending["vault_mode"] == 0o600
                assert Path(pending["vault_path"]).exists(), "fixture did not hold the Vault file"
                interrupted = "--signal" in sys.argv[2:]
                if interrupted:
                    proc.send_signal(signal.SIGTERM)
                else:
                    os.write(master, b"q")
                assert_exit(interrupted, pending_child)
                assert not Path(pending["vault_path"]).exists(), "pending preview left its Vault file after exit"
                assert not marker.exists(), "preview shutdown executed a playbook"
                print("PASS: pending Preview shutdown; owned child reaped, private Vault file removed, terminal restored")
                return
            send(b"2")
            send(b"/")
            send(b"jkhql/?")
            assert proc.poll() is None, "filter letters invoked global shortcuts"
            send(b"\x1b")
            send(b"\r")
            wait_text("site")
            send(b"\x1b")

            # Tags are a draft: text owns printable keys, and cancel is lossless.
            tag_start = len(output)
            send(b"t")
            wait_count(listings, 1, "catalogue")
            wait_text("ghost", since=tag_start)
            send(b"/")
            send(b"jkhql/?")
            assert proc.poll() is None, "tag filter letters invoked global actions"
            send(b"\x1b")  # Leave filter input.
            send(b"\x1b")  # Clear its query.
            send(b" ")     # Change only the draft.
            send(b"\x1b")  # Cancel draft.
            assert not marker.exists(), "tag cancellation executed work"

            send(b"t")
            wait_count(listings, 2, "catalogue")
            press(b"/", b"ghost", b"\r", b" ", b"\r")
            wait_text("Tags applied")
            unknown_start = len(output)
            send(b"t")
            wait_count(listings, 3, "catalogue")
            wait_text("not observed", since=unknown_start)
            send(b"\r")  # Applying must retain the unobserved selected tag.
            preview_start = len(output)
            send(b"p")
            rows = wait_count(listings, 1, "preview")
            assert rows[-1]["tags"] == ["ghost"], "unknown selected tag was lost"
            wait_text("Observed", since=preview_start)
            send(b"\r")
            wait_text("No tasks listed", since=preview_start)

            # Apply tags only marks this observation stale; p is the only refresh.
            old_previews = preview_count()
            stale_start = len(output)
            send(b"t")
            wait_count(listings, 4, "catalogue")
            press(b"A", b"/", b"greeting", b"\r", b" ", b"\r")
            wait_text("STALE", since=stale_start)
            read_for(0.35)
            assert preview_count() == old_previews, "tag apply refreshed preview automatically"
            preview_start = len(output)
            send(b"p")
            rows = wait_count(listings, old_previews + 1, "preview")
            assert rows[-1]["tags"] == ["greeting"]
            wait_text("Observed", since=preview_start)
            wait_text("Demo source task", since=preview_start)
            press(b"h", b"g", b"\r")  # Hosts section.
            wait_text("Pattern:")
            press(b"h", b"G", b"\r")  # Raw Ansible output section.
            send(b"G")
            wait_text("TASK TAGS:")

            # Session Vault input is private for successful and failed observations.
            send(b"V")
            press(b"PTY_VAULT_SECRET", b"\r")
            fail_preview.write_text("fail next observation")
            failure_start = len(output)
            send(b"p")
            failed = wait_count(listings, old_previews + 2, "preview")[-1]
            wait_text("Preview failed", since=failure_start)
            assert failed["vault_valid"] and failed["vault_mode"] == 0o600
            assert not Path(failed["vault_path"]).exists(), "failed preview left a Vault file"
            assert all(secret.encode() not in output for secret in ("PTY_VAULT_SECRET", "PTY_ERROR_SECRET", "URL_SECRET", "QUERY_SECRET")), "preview output exposed a credential"
            fail_preview.unlink()
            send(b"p")
            succeeded = wait_count(listings, old_previews + 3, "preview")[-1]
            read_for(0.25)
            assert succeeded["vault_valid"] and not Path(succeeded["vault_path"]).exists()

            # Role declarations and local sources remain in the same workspace.
            role_start = len(output)
            send(b"O")
            wait_text("Related declarations", since=role_start)
            wait_text("demo", since=role_start)
            press(b"/", b"demo", b"\r")
            send(b"f")
            wait_text("Role sources", since=role_start)
            press(b"j", b"\r")  # Declaration is first, tasks source is second.
            wait_text("Demo source task", since=role_start)
            send(b"\x1b")
            send(b"\x1b")
            send(b"2")
            send(b"r")
            wait_text("Cancel")
            send(b"\r")
            assert not marker.exists(), "initial Enter in review executed a run"
            send(b"r")
            read_for(0.5)
            send(b"\t\r")
            deadline = time.monotonic() + 5
            while not marker.exists() and time.monotonic() < deadline:
                read_for(0.1)
            assert marker.exists(), "approved run did not execute"
            send(b"2")  # Execution intentionally focuses Logs; inspect playbook help.
            help_start = len(output)
            send(b"?")
            wait_text("Keyboard shortcuts", since=help_start)
            wait_text("Enter / Space", since=help_start)
            send(b"G")
            wait_text("Text fields own printable keys", since=help_start)
            send(b"g")
            read_for(1.5)
            send(b"\x1b")
            # A second completed run proves the overlay did not swallow RunFinished.
            send(b"r")
            read_for(0.5)
            send(b"\t\r")
            read_for(1.7)
            runs = [json.loads(line) for line in marker.read_text().splitlines()]
            assert len(runs) == 2, f"expected 2 runs; got {len(runs)}"
            assert all(Path(r["cwd"]).resolve() == project.resolve() for r in runs)
            assert all(r["callback"] == "default" for r in runs)
            assert all(r["argv"][r["argv"].index("--tags") + 1] == "greeting" for r in runs), "executed tags differ from the reviewed preview"
            assert all(Path(r["cwd"]).resolve() == project.resolve() for r in records(listings)), "listing used another project"
            assert all(not Path(r["vault_path"]).exists() for r in records(listings) if "vault_path" in r), "observation leaked a Vault temporary file"
            send(b":")
            send(b"runtime")
            send(b"\r")
            wait_text("Shared uv tool")
            send(b"\x1b")
            send(b":")
            send(b"configuration")
            send(b"\r")
            wait_text("DEFAULT_HOST_LIST")
            send(b"\x1b")
            send(b"2")
            send(b"E")
            wait_text("EDITOR_HANDOFF_OK")
            read_for(0.3)
            for height, width in ((35, 120), (12, 35), (24, 80)):
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
                os.kill(proc.pid, signal.SIGWINCH)
                send(b"\t\x1b[Z")
            # Quit during a third run must cancel and reap the child before exit.
            send(b"2")
            send(b"r")
            read_for(0.5)
            send(b"\t\r")
            read_for(0.2)
            latest = [json.loads(line) for line in marker.read_text().splitlines()]
            assert len(latest) == 3, "cancellation fixture did not start"
            pending_child = latest[-1]["pid"]
            interrupted = "--signal" in sys.argv[2:]
            if interrupted:
                proc.send_signal(signal.SIGTERM)
            else:
                os.write(master, b"q")
            assert_exit(interrupted, pending_child)
            print("PASS: real PTY 80x24 / 120x35 / 35x12; tag draft cancel/apply/unknown retention, manual stale preview, hosts/tasks/output, role sources, private Vault observation lifecycle, review/run scope parity, overlay completion, Inspector, Runtime, editor, resize, child cancellation and terminal restoration")
        finally:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()
            if pending_child is not None:
                # Clean up only our identified disposable backend on a failed test.
                probe = subprocess.run(["/bin/ps", "-p", str(pending_child), "-o", "command="],
                                       text=True, capture_output=True)
                if root.name in probe.stdout:
                    try:
                        os.kill(pending_child, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
            os.close(master)
            os.close(slave)


if __name__ == "__main__":
    main()
