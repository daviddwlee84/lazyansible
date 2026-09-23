// Package paths owns lazyansible's XDG locations. Resolving paths never creates them.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func homePath(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

func xdg(name string, fallback ...string) string {
	if value := os.Getenv(name); filepath.IsAbs(value) {
		return filepath.Join(value, "lazyansible")
	}
	return homePath(append(fallback, "lazyansible")...)
}

func ConfigDir() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "lazyansible")
		}
		return ""
	}
	return xdg("XDG_CONFIG_HOME", ".config")
}

func StateDir() string {
	if runtime.GOOS == "windows" {
		if dir := os.Getenv("LOCALAPPDATA"); filepath.IsAbs(dir) {
			return filepath.Join(dir, "lazyansible", "state")
		}
		return ""
	}
	return xdg("XDG_STATE_HOME", ".local", "state")
}

func CacheDir() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserCacheDir(); err == nil {
			return filepath.Join(dir, "lazyansible", "cache")
		}
		return ""
	}
	return xdg("XDG_CACHE_HOME", ".cache")
}

func LegacyDir() string { return homePath(".lazyansible") }

// Join does not turn an unavailable base directory into a relative path.
func Join(base string, parts ...string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// ReadConfigFile uses a legacy file only when its XDG replacement is absent.
func ReadConfigFile(name string) ([]byte, error) {
	p := Join(ConfigDir(), name)
	if p == "" {
		return nil, fmt.Errorf("cannot locate config directory: HOME is unset")
	}
	data, err := os.ReadFile(p)
	if !os.IsNotExist(err) {
		return data, err
	}
	legacy := Join(LegacyDir(), name)
	if legacy == "" {
		return nil, err
	}
	return os.ReadFile(legacy)
}

// WriteFile atomically saves private application state at its destination.
func WriteFile(path string, data []byte) error {
	if path == "" {
		return fmt.Errorf("cannot save state: no home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".lazyansible-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, path)
}
