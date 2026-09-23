package roles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanCollectsExistingMainFilesAndSupportsYAML(t *testing.T) {
	root := t.TempDir()
	role := filepath.Join(root, "demo")
	for name, body := range map[string]string{"tasks/main.yaml": "- name: Hello\n  ansible.builtin.debug:\n    msg: hello\n", "defaults/main.yml": "message: default\n", "vars/main.yml": "other: true\n"} {
		path := filepath.Join(role, name)
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte(body), 0600)
	}
	found, err := ScanContext(context.Background(), root)
	if err != nil || len(found) != 1 {
		t.Fatalf("scan: %+v %v", found, err)
	}
	if len(found[0].Tasks) != 1 || found[0].Tasks[0].Name != "Hello" || found[0].Defaults["message"] != "default" || len(found[0].Sources) != 3 {
		t.Fatalf("metadata/sources: %+v", found[0])
	}
	data, err := ReadSource(context.Background(), found[0].Sources[0].Path)
	if err != nil || !strings.Contains(data, "Hello") {
		t.Fatalf("source: %s %v", data, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanContext(ctx, root); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := ReadSource(ctx, found[0].Sources[0].Path); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestReadSourceBoundsAndMissingRoles(t *testing.T) {
	root := t.TempDir()
	found, err := Scan(filepath.Join(root, "missing"))
	if err != nil || len(found) != 0 {
		t.Fatalf("missing roles not empty: %v %v", found, err)
	}
	if _, err := ReadSource(context.Background(), root); err == nil {
		t.Fatal("directory preview accepted")
	}
	path := filepath.Join(root, "large.yml")
	os.WriteFile(path, []byte(strings.Repeat("x", (1<<20)+1)), 0600)
	if _, err := ReadSource(context.Background(), path); err == nil {
		t.Fatal("unbounded source accepted")
	}
}
