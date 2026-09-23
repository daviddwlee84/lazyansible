package ssh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyFallbackAndPrivateXDGSave(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "config"))
	got, err := Load()
	if err != nil || len(got) != 0 {
		t.Fatalf("empty read: %v %v", got, err)
	}
	if entries, _ := os.ReadDir(h); len(entries) != 0 {
		t.Fatal("read created directories")
	}
	old := filepath.Join(h, ".lazyansible", "ssh-profiles.json")
	os.MkdirAll(filepath.Dir(old), 0700)
	os.WriteFile(old, []byte(`[{"name":"legacy","user":"tester"}]`), 0600)
	got, err = Load()
	if err != nil || len(got) != 1 || got[0].Name != "legacy" {
		t.Fatalf("legacy load: %v %v", got, err)
	}
	if err := Save([]*Profile{{Name: "new"}}); err != nil {
		t.Fatal(err)
	}
	got, err = Load()
	if err != nil || len(got) != 1 || got[0].Name != "new" {
		t.Fatalf("new load: %v %v", got, err)
	}
	p, _ := profilesPath()
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal("profile is not private")
	}
	if data, _ := os.ReadFile(old); string(data) != `[{"name":"legacy","user":"tester"}]` {
		t.Fatal("legacy file changed")
	}
}
