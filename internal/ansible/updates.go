package ansible

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type receiptIndex struct {
	Name         string `toml:"name"`
	URL          string `toml:"url"`
	Default      bool   `toml:"default"`
	Explicit     bool   `toml:"explicit"`
	Format       string `toml:"format"`
	Authenticate string `toml:"authenticate"`
}
type receiptOptions struct {
	Index           []receiptIndex `toml:"index"`
	IndexURL        string         `toml:"index-url"`
	ExtraIndexURL   []string       `toml:"extra-index-url"`
	IndexStrategy   string         `toml:"index-strategy"`
	KeyringProvider string         `toml:"keyring-provider"`
	ExcludeNewer    string         `toml:"exclude-newer"`
}

// receiptIndexArgs forwards only semantics supported by this small adapter.
// Unknown source/resolver settings are diagnosed, never silently replaced with
// public PyPI. The receipt is owned by uv and is never edited here.
func receiptIndexArgs(data []byte) ([]string, string, error) {
	var raw struct {
		Tool struct {
			Options map[string]any `toml:"options"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, "", errors.New("invalid uv tool receipt; cannot preserve update source settings")
	}
	allowed := map[string]bool{"index": true, "index-url": true, "extra-index-url": true, "index-strategy": true, "keyring-provider": true, "exclude-newer": true, "link-mode": true, "compile-bytecode": true, "resolution": true}
	for key := range raw.Tool.Options {
		if !allowed[key] {
			return nil, "", fmt.Errorf("update observation does not support uv receipt setting %q", redact(key))
		}
	}
	var receipt struct {
		Tool struct {
			Options receiptOptions `toml:"options"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(data, &receipt); err != nil {
		return nil, "", errors.New("unsupported uv tool receipt format; cannot preserve update source settings")
	}
	o := receipt.Tool.Options
	args := []string{}
	hosts := map[string]bool{}
	remember := func(value string) {
		u, err := url.Parse(value)
		if err == nil && u.Hostname() != "" {
			hosts[u.Hostname()] = true
		}
	}
	overridden := func(keys ...string) bool {
		for _, key := range keys {
			if _, ok := os.LookupEnv(key); ok {
				return true
			}
		}
		return false
	}
	if o.IndexURL != "" && !overridden("UV_DEFAULT_INDEX", "UV_INDEX_URL") {
		args = append(args, "--default-index", o.IndexURL)
		remember(o.IndexURL)
	}
	for _, index := range o.Index {
		if index.Name != "" || index.Explicit || (index.Format != "" && index.Format != "simple") || (index.Authenticate != "" && index.Authenticate != "auto") || index.URL == "" {
			return nil, "", errors.New("update observation does not support this uv index authentication or selection policy")
		}
		if index.Default {
			if !overridden("UV_DEFAULT_INDEX", "UV_INDEX_URL") {
				args = append(args, "--default-index", index.URL)
				remember(index.URL)
			}
		} else if !overridden("UV_INDEX") {
			args = append(args, "--index", index.URL)
			remember(index.URL)
		}
	}
	if !overridden("UV_EXTRA_INDEX_URL") {
		for _, index := range o.ExtraIndexURL {
			args = append(args, "--extra-index-url", index)
			remember(index)
		}
	}
	if o.IndexStrategy != "" && !overridden("UV_INDEX_STRATEGY") {
		args = append(args, "--index-strategy", o.IndexStrategy)
	}
	if o.KeyringProvider != "" && !overridden("UV_KEYRING_PROVIDER") {
		args = append(args, "--keyring-provider", o.KeyringProvider)
	}
	if o.ExcludeNewer != "" && !overridden("UV_EXCLUDE_NEWER") {
		args = append(args, "--exclude-newer", o.ExcludeNewer)
	}
	list := []string{}
	for h := range hosts {
		list = append(list, h)
	}
	sort.Strings(list)
	source := "uv configured indexes"
	if len(list) > 0 {
		source = "uv tool indexes: " + strings.Join(list, ", ")
	}
	return args, source, nil
}

type updateCache struct {
	Key    string       `json:"key"`
	Status UpdateStatus `json:"status"`
}

func cacheLocation(opts RuntimeOptions, key string) (string, error) {
	dir := opts.CacheDir
	if dir == "" {
		base := os.Getenv("XDG_CACHE_HOME")
		if base == "" || !filepath.IsAbs(base) {
			home, err := os.UserHomeDir()
			if err != nil || home == "" || !filepath.IsAbs(home) {
				return "", errors.New("cannot determine an absolute update cache directory: set HOME or XDG_CACHE_HOME")
			}
			base = filepath.Join(home, ".cache")
		}
		dir = filepath.Join(base, "lazyansible")
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("update cache directory must be absolute")
	}
	return filepath.Join(dir, "ansible-update-"+key[:16]+".json"), nil
}

func saveUpdate(path, key string, status UpdateStatus) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(updateCache{Key: key, Status: status})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ansible-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Check observes the latest owner-package release on the installation's uv
// indexes. It does not resolve an upgrade or claim a release satisfies a pin.
// A failed attempt is cached too, retaining the last successful observation.
func Check(ctx context.Context, opts RuntimeOptions, force bool) (UpdateStatus, error) {
	runtime, err := Resolve(ctx, opts)
	result := UpdateStatus{Runtime: runtime, State: "unknown"}
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if !runtime.Managed {
		if runtime.Ownership == "unknown" {
			result.Error = runtime.Issue
			return result, errors.New(result.Error)
		}
		result.State = "unmanaged"
		result.Error = runtime.Issue
		return result, nil
	}
	if runtime.Python == "" {
		err = errors.New("uv tool Python could not be identified")
		result.Error = err.Error()
		return result, err
	}
	receiptPath := filepath.Join(runtime.ToolDir, "uv-receipt.toml")
	info, err := os.Stat(receiptPath)
	if err != nil {
		result.Error = "uv tool receipt is unavailable"
		return result, errors.New(result.Error)
	}
	if info.Size() > 1<<20 {
		result.Error = "uv tool receipt is too large"
		return result, errors.New(result.Error)
	}
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		result.Error = "cannot read uv tool receipt"
		return result, errors.New(result.Error)
	}
	keyData := runtime.ToolDir + "\x00" + runtime.ToolPackage + "\x00" + runtime.ToolVersion + "\x00" + runtime.Python + "\x00" + string(receipt)
	for _, key := range []string{"UV_INDEX", "UV_DEFAULT_INDEX", "UV_INDEX_URL", "UV_EXTRA_INDEX_URL", "UV_INDEX_STRATEGY", "UV_CONFIG_FILE"} {
		keyData += "\x00" + key + "=" + os.Getenv(key)
	}
	hash := sha256.Sum256([]byte(keyData))
	key := hex.EncodeToString(hash[:])
	path, err := cacheLocation(opts, key)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	var previous updateCache
	if data, readErr := os.ReadFile(path); readErr == nil {
		_ = json.Unmarshal(data, &previous)
	}
	interval := opts.CheckInterval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if !force && previous.Key == key && !previous.Status.LastAttempt.IsZero() && time.Since(previous.Status.LastAttempt) >= 0 && time.Since(previous.Status.LastAttempt) < interval {
		result = previous.Status
		result.Runtime = runtime
		result.Cached = true
		if result.State == "unknown" && result.Error != "" {
			return result, errors.New(result.Error)
		}
		return result, nil
	}
	if previous.Key == key {
		result = previous.Status
		result.Runtime = runtime
		result.Cached = false
	}
	result.LastAttempt = time.Now()
	result.Source = ""
	result.Error = ""
	result.State = "unknown"
	indexArgs, source, checkErr := receiptIndexArgs(receipt)
	result.Source = source
	if checkErr == nil {
		var latest string
		latest, checkErr = observeLatest(ctx, runtime, indexArgs)
		if checkErr == nil {
			result.State = "current"
			if latest != "" {
				result.State = "available"
			}
			result.LatestVersion = latest
			result.CheckedAt = time.Now()
			result.Stale = false
		}
	}
	if checkErr != nil {
		result.State = "unknown"
		result.Error = redact(checkErr.Error())
		result.Stale = !result.CheckedAt.IsZero()
	}
	if err := saveUpdate(path, key, result); err != nil && result.Error == "" {
		result.Error = "update result could not be cached"
	}
	return result, checkErr
}

type packageObservation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Latest  string `json:"latest_version"`
}

func observeLatest(ctx context.Context, runtime RuntimeStatus, indexArgs []string) (string, error) {
	base := []string{"pip", "list", "--format", "json", "--python", runtime.Python, "--color", "never", "--no-progress", "--no-python-downloads"}
	spec := CommandSpec{Executable: runtime.UVExecutable, Args: base, Dir: runtime.ToolDir}
	out, diagnostic, err := capture(ctx, spec, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("inspect uv tool packages: %w: %s", err, diagnostic)
	}
	var packages []packageObservation
	if err = json.Unmarshal(out, &packages); err != nil {
		return "", errors.New("uv returned invalid package JSON")
	}
	found := false
	spec.Args = append(append([]string{}, base...), "--outdated")
	spec.Args = append(spec.Args, indexArgs...)
	for _, p := range packages {
		if p.Name == runtime.ToolPackage {
			if p.Version != runtime.ToolVersion {
				return "", errors.New("Ansible version changed during update observation; refresh again")
			}
			found = true
		} else {
			if p.Name == "" || strings.HasPrefix(p.Name, "-") {
				return "", errors.New("uv returned an invalid package name")
			}
			spec.Args = append(spec.Args, "--exclude", p.Name)
		}
	}
	if !found {
		return "", errors.New("owner package is missing from its uv tool environment")
	}
	out, diagnostic, err = capture(ctx, spec, 25*time.Second)
	if err != nil {
		return "", fmt.Errorf("check Ansible updates: %w: %s", err, diagnostic)
	}
	packages = nil
	if err = json.Unmarshal(out, &packages); err != nil {
		return "", errors.New("uv returned invalid update JSON")
	}
	if len(packages) == 0 {
		return "", nil
	}
	if len(packages) != 1 || packages[0].Name != runtime.ToolPackage || packages[0].Latest == "" {
		return "", errors.New("uv returned an unexpected scoped update result")
	}
	return packages[0].Latest, nil
}
