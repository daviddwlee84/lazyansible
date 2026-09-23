package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestXDGPathsAndRelativeFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG policy is macOS/Linux")
	}
	h := t.TempDir()
	t.Setenv("HOME", h)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, "relative/ignored")
	}
	if ConfigDir() != filepath.Join(h, ".config", "lazyansible") {
		t.Fatal(ConfigDir())
	}
	if StateDir() != filepath.Join(h, ".local", "state", "lazyansible") {
		t.Fatal(StateDir())
	}
	if CacheDir() != filepath.Join(h, ".cache", "lazyansible") {
		t.Fatal(CacheDir())
	}
	absolute := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", absolute)
	if ConfigDir() != filepath.Join(absolute, "lazyansible") {
		t.Fatal(ConfigDir())
	}
	if entries, _ := os.ReadDir(h); len(entries) != 0 {
		t.Fatal("path lookup wrote files")
	}
}
func TestMissingHomeNeverWritesCWD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows home contract differs")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if ConfigDir() != "" || Join("", "profiles.json") != "" {
		t.Fatal("missing home became relative path")
	}
	if err := WriteFile("", []byte("x")); err == nil {
		t.Fatal("missing destination accepted")
	}
}
