package ansible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func decodeObservation(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("unexpected data after JSON observation")
	}
	return nil
}

func sensitiveObservationKey(key string) bool {
	key = strings.ToLower(key)
	compact := strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(key)
	for _, part := range []string{"password", "passwd", "token", "secret", "apikey", "privatekey"} {
		if strings.Contains(compact, part) {
			return true
		}
	}
	return false
}

// Inventory/config are observational views, not a secret retrieval interface.
// Preserve JSON scalar types (including exact numbers) while masking values
// under common credential keys at any nesting level.
func safeObservationValue(key string, value any) any {
	if sensitiveObservationKey(key) {
		return "[redacted]"
	}
	switch value := value.(type) {
	case map[string]any:
		for k, v := range value {
			value[k] = safeObservationValue(k, v)
		}
		return value
	case []any:
		for i, v := range value {
			value[i] = safeObservationValue("", v)
		}
		return value
	case string:
		return redact(ansi.Strip(value))
	default:
		return value
	}
}

func safeObservationVars(vars map[string]any) map[string]any {
	if vars == nil {
		return nil
	}
	return safeObservationValue("", vars).(map[string]any)
}

func InspectInventory(ctx context.Context, p ProjectContext) (InventorySnapshot, error) {
	snapshot := InventorySnapshot{Hosts: []InventoryHost{}, Groups: []InventoryGroup{}}
	p, err := normalizeProject(p)
	if err != nil {
		return snapshot, err
	}
	snapshot.Project = p
	runtime, err := Resolve(ctx, RuntimeOptions{Executable: p.Executable, WorkDir: p.WorkDir})
	if err != nil {
		return snapshot, err
	}
	bin, err := Companion("ansible-inventory", runtime.Executable)
	if err != nil {
		return snapshot, err
	}
	args := []string{"--list"}
	if p.Inventory != "" {
		args = append(args, "-i", p.Inventory)
	}
	if p.PlaybookDir != "" {
		args = append(args, "--playbook-dir", p.PlaybookDir)
	}
	out, warning, err := capture(ctx, CommandSpec{Executable: bin, Args: args, Dir: p.WorkDir}, 20*time.Second)
	snapshot.Warnings = warning
	snapshot.ObservedAt = time.Now()
	if err != nil {
		return snapshot, fmt.Errorf("inspect inventory: %w: %s", err, warning)
	}
	var data map[string]json.RawMessage
	if err = decodeObservation(out, &data); err != nil {
		return snapshot, fmt.Errorf("invalid Ansible inventory JSON: %w", err)
	}
	hosts := map[string]*InventoryHost{}
	if meta, ok := data["_meta"]; ok {
		var m struct {
			Hostvars map[string]map[string]any `json:"hostvars"`
		}
		if err = decodeObservation(meta, &m); err != nil {
			return snapshot, fmt.Errorf("invalid inventory metadata: %w", err)
		}
		for name, vars := range m.Hostvars {
			hosts[name] = &InventoryHost{Name: name, Vars: safeObservationVars(vars), Groups: []string{}}
		}
	}
	for name, raw := range data {
		if name == "_meta" {
			continue
		}
		var g InventoryGroup
		if err = decodeObservation(raw, &g); err != nil {
			return snapshot, fmt.Errorf("invalid inventory group %s: %w", name, err)
		}
		g.Name = name
		g.Vars = safeObservationVars(g.Vars)
		if g.Hosts == nil {
			g.Hosts = []string{}
		}
		if g.Children == nil {
			g.Children = []string{}
		}
		sort.Strings(g.Hosts)
		sort.Strings(g.Children)
		for _, name := range g.Hosts {
			host := hosts[name]
			if host == nil {
				host = &InventoryHost{Name: name, Groups: []string{}}
				hosts[name] = host
			}
			host.Groups = append(host.Groups, g.Name)
		}
		snapshot.Groups = append(snapshot.Groups, g)
	}
	for _, h := range hosts {
		sort.Strings(h.Groups)
		snapshot.Hosts = append(snapshot.Hosts, *h)
	}
	sort.Slice(snapshot.Hosts, func(i, j int) bool { return snapshot.Hosts[i].Name < snapshot.Hosts[j].Name })
	sort.Slice(snapshot.Groups, func(i, j int) bool { return snapshot.Groups[i].Name < snapshot.Groups[j].Name })
	return snapshot, nil
}

func InspectConfig(ctx context.Context, p ProjectContext) (ConfigSnapshot, error) {
	snapshot := ConfigSnapshot{Entries: []ConfigEntry{}}
	p, err := normalizeProject(p)
	if err != nil {
		return snapshot, err
	}
	snapshot.Project = p
	runtime, err := Resolve(ctx, RuntimeOptions{Executable: p.Executable, WorkDir: p.WorkDir})
	if err != nil {
		return snapshot, err
	}
	bin, err := Companion("ansible-config", runtime.Executable)
	if err != nil {
		return snapshot, err
	}
	out, warning, err := capture(ctx, CommandSpec{Executable: bin, Args: []string{"dump", "--only-changed", "--format", "json"}, Dir: p.WorkDir}, 15*time.Second)
	snapshot.Warnings = warning
	snapshot.ObservedAt = time.Now()
	if err != nil {
		return snapshot, fmt.Errorf("inspect configuration: %w: %s", err, warning)
	}
	if err = decodeObservation(out, &snapshot.Entries); err != nil {
		return snapshot, fmt.Errorf("invalid Ansible configuration JSON: %w", err)
	}
	for i := range snapshot.Entries {
		entry := &snapshot.Entries[i]
		entry.Value = safeObservationValue(entry.Name, entry.Value)
		entry.Origin = redact(ansi.Strip(entry.Origin))
		entry.Name = ansi.Strip(entry.Name)
		entry.Type = ansi.Strip(entry.Type)
	}
	sort.Slice(snapshot.Entries, func(i, j int) bool { return snapshot.Entries[i].Name < snapshot.Entries[j].Name })
	return snapshot, nil
}
