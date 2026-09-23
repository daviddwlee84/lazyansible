package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daviddwlee84/lazyansible/internal/paths"
)

func configHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("LAZYANSIBLE_CONFIG", "")
	return h
}
func TestResolvePrecedenceAndReadOnly(t *testing.T) {
	h := configHome(t)
	want := filepath.Join(h, ".config", "lazyansible", "config.yml")
	got, legacy := ResolvePath("")
	if got != want || legacy {
		t.Fatalf("default=%q legacy=%v", got, legacy)
	}
	cfg, err := Load(got)
	if err != nil || !cfg.CheckUpdates {
		t.Fatalf("missing config: %+v %v", cfg, err)
	}
	if _, err := os.Stat(paths.ConfigDir()); !os.IsNotExist(err) {
		t.Fatal("read created config directory")
	}
	old := filepath.Join(h, ".lazyansible", "config.yml")
	if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("no_mouse: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, legacy = ResolvePath("")
	if got != old || !legacy {
		t.Fatal("legacy fallback missing")
	}
	if err := WriteExample(want); err != nil {
		t.Fatal(err)
	}
	got, legacy = ResolvePath("")
	if got != want || legacy {
		t.Fatal("XDG did not win")
	}
	t.Setenv("LAZYANSIBLE_CONFIG", filepath.Join(h, "environment.yml"))
	got, _ = ResolvePath("")
	if got != os.Getenv("LAZYANSIBLE_CONFIG") {
		t.Fatal("environment did not win")
	}
	got, _ = ResolvePath("explicit.yml")
	if got != "explicit.yml" {
		t.Fatal("explicit did not win")
	}
}
func TestInitNeverOverwritesAndLoadValidates(t *testing.T) {
	h := configHome(t)
	p := filepath.Join(h, "config.yml")
	if err := WriteExample(p); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)
	if err := WriteExample(p); !os.IsExist(err) {
		t.Fatalf("overwrite returned %v", err)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("config changed")
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	for _, body := range []string{"unknown_setting: true\n", "no_mouse: wrong\n", "{}\n---\n{}\n", "runtime:\n  package: pip\n"} {
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
	if err := os.WriteFile(p, []byte("check_updates: false\nno_mouse: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.CheckUpdates || !cfg.NoMouse {
		t.Fatalf("explicit bool: %+v %v", cfg, err)
	}
}
