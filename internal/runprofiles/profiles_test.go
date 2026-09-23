package runprofiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileMigrationIsReadOnlyUntilSave(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "config"))
	got, err := Load()
	if err != nil || len(got) != 0 {
		t.Fatalf("missing: %v %v", got, err)
	}
	if entries, _ := os.ReadDir(h); len(entries) != 0 {
		t.Fatal("read wrote state")
	}
	old := filepath.Join(h, ".lazyansible", "run-profiles.json")
	os.MkdirAll(filepath.Dir(old), 0700)
	os.WriteFile(old, []byte(`[{"name":"old","work_dir":"/project"}]`), 0600)
	got, err = Load()
	if err != nil || len(got) != 1 || got[0].WorkDir != "/project" {
		t.Fatalf("legacy: %v %v", got, err)
	}
	if err := Save(got); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(storePath())
	if info.Mode().Perm() != 0600 {
		t.Fatal("profile is not private")
	}
	if err := Save(nil); err != nil {
		t.Fatal(err)
	}
	got, err = Load()
	if err != nil || len(got) != 0 {
		t.Fatalf("explicit empty resurrected legacy: %v %v", got, err)
	}
}
