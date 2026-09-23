#!/usr/bin/env python3
"""Exercise the real terminal parser with disposable state and fake Ansible.

Usage: python3 testdata/pty_smoke.py /absolute/path/to/lazyansible
No Python dependencies, network access, or real host operations are required.
"""
import fcntl
import json
import os
from pathlib import Path
import pty
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
        (project / "site.yml").write_text("- name: fixture\n  hosts: web\n  gather_facts: false\n  tasks: []\n")
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
else:
 with open(os.environ['LAZYANSIBLE_SMOKE_MARKER'], 'a') as f:
  f.write(json.dumps({'cwd': os.getcwd(), 'argv': sys.argv[1:], 'callback': os.environ.get('ANSIBLE_STDOUT_CALLBACK'), 'pid': os.getpid()}) + '\n')
 for line in ['PLAY [fixture]', 'TASK [start]', 'ok: [fixture]', 'TASK [finish]', 'changed: [fixture]', 'PLAY RECAP', 'fixture : ok=2 changed=1 failed=0']:
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
        env = os.environ.copy()
        for key in list(env):
            if key.startswith(("ANSIBLE_", "UV_", "LAZYANSIBLE_")):
                del env[key]
        env.update(HOME=tmp, XDG_CONFIG_HOME=str(root / "config"), XDG_STATE_HOME=str(root / "state"),
                   XDG_CACHE_HOME=str(root / "cache"), XDG_DATA_HOME=str(root / "data"),
                   PATH=str(tools) + ":/usr/bin:/bin", TERM="xterm-256color", NO_COLOR="1",
                   VISUAL=str(edit), EDITOR=str(edit), LAZYANSIBLE_SMOKE_MARKER=str(marker))
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

        def wait_text(text, timeout=8, since=0):
            deadline = time.monotonic() + timeout
            while text.encode() not in output[since:]:
                if time.monotonic() > deadline:
                    raise AssertionError(f"did not see {text!r}: " + output.decode(errors="replace")[-2000:])
                read_for(0.1)

        try:
            wait_text("Inventory")
            read_for(0.5)
            send(b"2")
            send(b"/")
            send(b"jkhql/?")
            assert proc.poll() is None, "filter letters invoked global shortcuts"
            send(b"\x1b")
            send(b"\r")
            wait_text("site")
            send(b"\x1b")
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
            interrupted = len(sys.argv) > 2 and sys.argv[2] == "--signal"
            if interrupted:
                proc.send_signal(signal.SIGTERM)
            else:
                os.write(master, b"q")
            proc.wait(timeout=5)
            try:
                os.kill(latest[-1]["pid"], 0)
            except ProcessLookupError:
                pass
            else:
                raise AssertionError("Ansible child survived dashboard exit")
            read_for(0.1)
            assert proc.returncode == (130 if interrupted else 0), f"exit {proc.returncode}"
            after = termios.tcgetattr(slave)
            mask = termios.ECHO | termios.ICANON | termios.ISIG
            assert after[3] & mask == before[3] & mask, "terminal input mode was not restored"
            assert b"\x1b[?1049l" in output, "alternate screen was not released"
            print("PASS: real PTY 80x24 / 120x35 / 35x12; text ownership, review cancel/run, overlay completion, Inspector, Runtime, editor return, resize, active-child cancellation and terminal restoration")
        finally:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()
            os.close(master)
            os.close(slave)


if __name__ == "__main__":
    main()
