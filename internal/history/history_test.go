package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryMergedWithXDGPrecedence(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	got, err := Load()
	if err != nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
	if entries, _ := os.ReadDir(h); len(entries) != 0 {
		t.Fatal("read created state")
	}
	legacy := filepath.Join(h, ".lazyansible", "history")
	os.MkdirAll(legacy, 0700)
	now := time.Now()
	r := Record{ID: "same", PlaybookName: "legacy", StartTime: now, EndTime: now}
	data, _ := json.Marshal(r)
	os.WriteFile(filepath.Join(legacy, "old.json"), data, 0600)
	r.PlaybookName = "XDG"
	r.WorkDir = "/project"
	if err := Save(&r); err != nil {
		t.Fatal(err)
	}
	earlier := Record{ID: "old", StartTime: now.Add(-time.Hour)}
	data, _ = json.Marshal(earlier)
	os.WriteFile(filepath.Join(legacy, "earlier.json"), data, 0600)
	got, err = Load()
	if err != nil || len(got) != 2 || got[0].PlaybookName != "XDG" || got[1].ID != "old" {
		t.Fatalf("merge: %+v %v", got, err)
	}
	dir := filepath.Join(h, "state", "lazyansible", "history")
	entries, _ := os.ReadDir(dir)
	info, _ := entries[0].Info()
	if info.Mode().Perm() != 0600 {
		t.Fatalf("history mode %o", info.Mode().Perm())
	}
}
