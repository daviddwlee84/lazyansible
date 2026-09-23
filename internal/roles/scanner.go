// Package roles scans Ansible role directories and parses their metadata.
package roles

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Task is a single task parsed from a role's tasks file.
type Task struct {
	Name   string
	Module string // best-guess module name
	Tags   []string
}

// Role holds the metadata for a single Ansible role.
type Role struct {
	Name     string
	Path     string
	Desc     string // from meta/main.yml galaxy_info.description
	Tasks    []Task
	Defaults map[string]string // from defaults/main.yml
	Handlers []string          // handler names from handlers/main.yml
	Deps     []string          // role dependencies from meta/main.yml
	Sources  []SourceFile      // existing main source files, in UI display order
}

type SourceFile struct {
	Kind string
	Path string
	Line int // initial one-based source line; zero means start of file
}

// Scan finds all roles under rolesDir and parses their metadata.
func Scan(rolesDir string) ([]*Role, error) {
	return ScanContext(context.Background(), rolesDir)
}

// ScanContext performs all filesystem discovery outside the UI message loop.
func ScanContext(ctx context.Context, rolesDir string) ([]*Role, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(rolesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read roles dir %s: %w", rolesDir, err)
	}

	var roles []*Role
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		rolePath := filepath.Join(rolesDir, e.Name())
		r := parseRole(rolePath, e.Name())
		roles = append(roles, r)
	}

	sort.Slice(roles, func(i, j int) bool {
		return roles[i].Name < roles[j].Name
	})
	return roles, nil
}

// ─── Internal parsers ────────────────────────────────────────────────────────

func parseRole(path, name string) *Role {
	r := &Role{
		Name:     name,
		Path:     path,
		Defaults: make(map[string]string),
	}
	for _, kind := range []string{"tasks", "defaults", "vars", "handlers", "meta"} {
		for _, name := range []string{"main.yml", "main.yaml"} {
			p := filepath.Join(path, kind, name)
			if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
				r.Sources = append(r.Sources, SourceFile{Kind: kind, Path: p})
				break
			}
		}
	}
	for _, source := range r.Sources {
		switch source.Kind {
		case "tasks":
			r.Tasks = parseTasks(source.Path)
		case "defaults":
			r.Defaults = parseDefaults(source.Path)
		case "handlers":
			r.Handlers = parseHandlerNames(source.Path)
		case "meta":
			r.Desc, r.Deps = parseMeta(source.Path)
		}
	}
	return r
}

// ReadSource returns bounded local source text. It never follows includes or
// evaluates variables, and callers run it as an asynchronous effect.
func ReadSource(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source is not a regular file: %s", path)
	}
	const limit = 1 << 20
	if info.Size() > limit {
		return "", fmt.Errorf("source exceeds 1 MiB preview limit: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("source exceeds 1 MiB preview limit: %s", path)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(data), nil
}

// parseTasks parses tasks/main.yml into a slice of Task.
func parseTasks(path string) []Task {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw []map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}

	var tasks []Task
	for _, item := range raw {
		t := Task{}
		if n, ok := item["name"].(string); ok {
			t.Name = n
		}
		t.Module = guessModule(item)
		t.Tags = extractStringSlice(item["tags"])
		tasks = append(tasks, t)
	}
	return tasks
}

// guessModule picks the Ansible module key from a task map.
func guessModule(task map[string]interface{}) string {
	skip := map[string]bool{
		"name": true, "tags": true, "when": true, "loop": true,
		"with_items": true, "register": true, "become": true,
		"become_user": true, "notify": true, "ignore_errors": true,
		"failed_when": true, "changed_when": true, "no_log": true,
		"vars": true, "environment": true, "delegate_to": true,
		"run_once": true, "any_errors_fatal": true, "block": true,
		"rescue": true, "always": true, "loop_control": true,
	}
	for k := range task {
		if !skip[k] {
			// Strip FQCN prefix (ansible.builtin.apt → apt).
			parts := strings.Split(k, ".")
			return parts[len(parts)-1]
		}
	}
	return ""
}

// parseDefaults parses defaults/main.yml into a string map.
func parseDefaults(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

// parseHandlerNames extracts handler names from handlers/main.yml.
func parseHandlerNames(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw []map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	var names []string
	for _, item := range raw {
		if n, ok := item["name"].(string); ok {
			names = append(names, n)
		}
	}
	return names
}

// metaDoc is a minimal representation of meta/main.yml.
type metaDoc struct {
	GalaxyInfo struct {
		Description string `yaml:"description"`
	} `yaml:"galaxy_info"`
	Dependencies []interface{} `yaml:"dependencies"`
}

// parseMeta extracts role description and dependencies from meta/main.yml.
func parseMeta(path string) (desc string, deps []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc metaDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return
	}
	desc = doc.GalaxyInfo.Description
	for _, d := range doc.Dependencies {
		switch v := d.(type) {
		case string:
			deps = append(deps, v)
		case map[string]interface{}:
			if role, ok := v["role"].(string); ok {
				deps = append(deps, role)
			}
		}
	}
	return
}

func extractStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []interface{}:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
