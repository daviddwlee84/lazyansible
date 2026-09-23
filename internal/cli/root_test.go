package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/daviddwlee84/lazyansible/internal/config"
	"github.com/daviddwlee84/lazyansible/internal/editor"
	"github.com/daviddwlee84/lazyansible/internal/ui"
)

func cliHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("LAZYANSIBLE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, "cache"))
	return h
}
func command(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	cmd := NewRootCommand(Options{In: strings.NewReader(""), Out: &out, Err: &diagnostic, IsTerminal: func() bool { return false }})
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), diagnostic.String(), err
}
func TestStaticPathsIgnoreMalformedConfigAndDoNotWrite(t *testing.T) {
	h := cliHome(t)
	bad := filepath.Join(h, "bad.yml")
	os.WriteFile(bad, []byte("[: broken"), 0600)
	for _, args := range [][]string{{"--config", bad, "--help"}, {"--config", bad, "--version"}, {"--config", bad, "version", "--json"}, {"config", "--help"}, {"runtime", "--help"}} {
		out, _, err := command(t, args...)
		if err != nil || out == "" {
			t.Fatalf("%v: %q %v", args, out, err)
		}
	}
	entries, _ := os.ReadDir(h)
	if len(entries) != 1 {
		t.Fatalf("static command created state: %v", entries)
	}
	if _, _, err := command(t, "--unknown"); err == nil {
		t.Fatal("unknown flag accepted")
	}
}
func TestExplicitFalseAndProjectConfig(t *testing.T) {
	h := cliHome(t)
	p := filepath.Join(h, "settings.yml")
	os.WriteFile(p, []byte("no_mouse: true\nnotify_on_finish: true\ndefault_check_mode: true\ndefault_diff_mode: true\ncheck_updates: true\ninventory: inventories/local.ini\nplaybook_dir: playbooks\n"), 0600)
	out, _, err := command(t, "--config", p, "-C", h, "config", "show", "--json", "--no-mouse=false", "--notify=false", "--check=false", "--diff=false", "--check-updates=false")
	if err != nil {
		t.Fatal(err)
	}
	var e effective
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatal(err)
	}
	if e.Config.NoMouse || e.Config.NotifyOnFinish || e.Config.DefaultCheckMode || e.Config.DefaultDiffMode || e.Config.CheckUpdates {
		t.Fatalf("false ignored: %+v", e.Config)
	}
	if e.Config.Inventory != filepath.Join(h, "inventories/local.ini") || e.Config.PlaybookDir != filepath.Join(h, "playbooks") {
		t.Fatalf("project paths: %+v", e)
	}
	out, _, err = command(t, "--config", p, "config", "show", "--json", "-i", "localhost,")
	if err != nil || !strings.Contains(out, `"inventory": "localhost,"`) {
		t.Fatalf("inline inventory: %s %v", out, err)
	}
	if _, _, err = command(t, "--config", filepath.Join(h, "missing"), "config", "show"); err == nil {
		t.Fatal("explicit missing config accepted")
	}
}
func TestTTYPolicyAndDashboardDefaults(t *testing.T) {
	h := cliHome(t)
	p := filepath.Join(h, "settings.yml")
	os.WriteFile(p, []byte("default_check_mode: true\ndefault_diff_mode: true\n"), 0600)
	if _, _, err := command(t); err == nil {
		t.Fatal("non-TTY started dashboard")
	}
	called := false
	cmd := NewRootCommand(Options{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, IsTerminal: func() bool { return true }, Dashboard: func(c ui.Config, cfg config.Config) error {
		called = true
		if !c.DefaultCheckMode || !c.DefaultDiffMode {
			t.Fatal("defaults missing")
		}
		return nil
	}})
	cmd.SetArgs([]string{"--config", p})
	if err := cmd.Execute(); err != nil || !called {
		t.Fatalf("dashboard: called=%v err=%v", called, err)
	}
	called = false
	cmd = NewRootCommand(Options{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, IsTerminal: func() bool { return true }, Dashboard: func(ui.Config, config.Config) error { called = true; return nil }})
	cmd.SetArgs([]string{"--json"})
	if err := cmd.Execute(); err == nil || called {
		t.Fatal("JSON opened dashboard")
	}
}
func fakeAnsible(t *testing.T, h string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake process")
	}
	bin := filepath.Join(h, "bin")
	os.MkdirAll(bin, 0700)
	marker := filepath.Join(h, "executed")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'ansible [core 2.20.5]'; exit 0; fi\nprintf '%s\\n' \"$PWD\" > \"$LAZYANSIBLE_TEST_MARKER\"\nprintf '%s\\n' \"$@\"\nexit \"${LAZYANSIBLE_TEST_EXIT:-0}\"\n"
	for _, name := range []string{"ansible", "ansible-playbook"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("LAZYANSIBLE_TEST_MARKER", marker)
	playbook := filepath.Join(h, "test.yml")
	os.WriteFile(playbook, []byte("- hosts: localhost\n  gather_facts: false\n  tasks: []\n"), 0600)
	return playbook, marker
}
func TestRunPlanRedactionAndExplicitExecution(t *testing.T) {
	h := cliHome(t)
	playbook, marker := fakeAnsible(t, h)
	out, _, err := command(t, "-C", h, "run", playbook, "--dry-run", "--json", "--extra-vars", "secret=DO_NOT_SHOW")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(out)) || strings.Contains(out, "DO_NOT_SHOW") {
		t.Fatalf("unsafe plan: %s", out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("dry run executed")
	}
	if _, _, err := command(t, "-C", h, "run", playbook); err == nil {
		t.Fatal("non-TTY silently executed")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("missing yes executed")
	}
	if _, _, err := command(t, "-C", h, "run", playbook, "--json", "--yes"); err == nil {
		t.Fatal("JSON executed mutation")
	}
	if _, _, err := command(t, "-C", h, "run", playbook, "--yes", "--check"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(marker)
	wantDir, _ := filepath.EvalSymlinks(h)
	if strings.TrimSpace(string(data)) != wantDir {
		t.Fatalf("cwd=%s", data)
	}
}
func TestEditorArgumentsAreNotShellEvaluated(t *testing.T) {
	cmd, err := editor.Command(`code --wait "$(touch sentinel)"`, "file with spaces.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Args) != 4 || cmd.Args[2] != "$(touch sentinel)" || cmd.Args[3] != "file with spaces.yml" {
		t.Fatalf("args=%q", cmd.Args)
	}
	if _, err := editor.Command(`vim "unterminated`, "x"); err == nil {
		t.Fatal("invalid editor accepted")
	}
}

func TestChildExitStatusIsPreserved(t *testing.T) {
	h := cliHome(t)
	playbook, _ := fakeAnsible(t, h)
	t.Setenv("LAZYANSIBLE_TEST_EXIT", "7")
	var out, diagnostic bytes.Buffer
	code := Execute(context.Background(), []string{"-C", h, "run", playbook, "--yes"}, strings.NewReader(""), &out, &diagnostic)
	if code != 7 {
		t.Fatalf("exit=%d stderr=%s", code, diagnostic.String())
	}
}
