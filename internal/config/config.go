// Package config loads typed preferences without creating files on reads.
package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/daviddwlee84/lazyansible/internal/paths"
	"gopkg.in/yaml.v3"
)

type Runtime struct {
	Executable   string `yaml:"executable,omitempty" json:"executable,omitempty"`
	UVExecutable string `yaml:"uv_executable,omitempty" json:"uv_executable,omitempty"`
	Package      string `yaml:"package,omitempty" json:"package,omitempty"`
}

type Config struct {
	Inventory        string  `yaml:"inventory,omitempty" json:"inventory,omitempty"`
	PlaybookDir      string  `yaml:"playbook_dir,omitempty" json:"playbook_dir,omitempty"`
	NoMouse          bool    `yaml:"no_mouse" json:"no_mouse"`
	NotifyOnFinish   bool    `yaml:"notify_on_finish" json:"notify_on_finish"`
	DefaultCheckMode bool    `yaml:"default_check_mode" json:"default_check_mode"`
	DefaultDiffMode  bool    `yaml:"default_diff_mode" json:"default_diff_mode"`
	CheckUpdates     bool    `yaml:"check_updates" json:"check_updates"`
	Runtime          Runtime `yaml:"runtime,omitempty" json:"runtime"`
}

func Defaults() Config { return Config{CheckUpdates: true} }

// DefaultPath is the destination for new preferences, never a legacy fallback.
func DefaultPath() string {
	if p := os.Getenv("LAZYANSIBLE_CONFIG"); p != "" {
		return p
	}
	return paths.Join(paths.ConfigDir(), "config.yml")
}

// ResolvePath selects a read path; explicit and environment paths never fall back.
func ResolvePath(explicit string) (string, bool) {
	if explicit != "" {
		return explicit, false
	}
	p := DefaultPath()
	if os.Getenv("LAZYANSIBLE_CONFIG") != "" {
		return p, false
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		return p, false
	}
	legacy := paths.Join(paths.LegacyDir(), "config.yml")
	if _, err := os.Stat(legacy); err == nil {
		return legacy, true
	}
	return p, false
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	if path == "" {
		return cfg, fmt.Errorf("cannot locate config: HOME is unset")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&cfg); err != nil && err != io.EOF {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return cfg, fmt.Errorf("config %s: expected one YAML document", path)
	}
	if cfg.Runtime.Package != "" && cfg.Runtime.Package != "ansible-core" && cfg.Runtime.Package != "ansible" {
		return cfg, fmt.Errorf("config %s: runtime.package must be ansible-core or ansible", path)
	}
	return cfg, nil
}

const Example = `# lazyansible preferences. Explicit CLI flags override these values.
# macOS/Linux: $XDG_CONFIG_HOME/lazyansible/config.yml (default ~/.config).
# Relative project paths are resolved against the selected --chdir.
# inventory: ./inventories/localhost.ini
# playbook_dir: ./playbooks
no_mouse: false
notify_on_finish: false
default_check_mode: false
default_diff_mode: false
check_updates: true
# The existing uv tool owns its version and Python constraints.
# runtime:
#   executable: /path/to/ansible-playbook
#   uv_executable: /path/to/uv
#   package: ansible-core
`

// WriteExample creates a private config and never overwrites an existing file.
func WriteExample(path string) error {
	if path == "" {
		return fmt.Errorf("cannot initialize config: HOME is unset")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(f, Example)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
