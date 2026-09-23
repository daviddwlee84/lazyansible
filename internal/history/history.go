// Package history persists private run records in XDG state and reads legacy history.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/paths"
)

// Record holds metadata about a single playbook or ad-hoc run.
type Record struct {
	ID            string              `json:"id"`
	WorkDir       string              `json:"work_dir,omitempty"`
	RolePath      string              `json:"role_path,omitempty"`
	Request       *ansible.RunRequest `json:"request,omitempty"`
	RequiresInput bool                `json:"requires_input,omitempty"`
	Kind          string              `json:"kind"` // "playbook" | "adhoc"
	PlaybookName  string              `json:"playbook_name"`
	PlaybookPath  string              `json:"playbook_path"`
	Inventory     string              `json:"inventory"`
	Limit         string              `json:"limit,omitempty"`
	Tags          string              `json:"tags,omitempty"`
	ExtraVars     string              `json:"extra_vars,omitempty"`
	CheckMode     bool                `json:"check_mode,omitempty"`
	DiffMode      bool                `json:"diff_mode,omitempty"`
	Module        string              `json:"module,omitempty"` // ad-hoc
	Args          string              `json:"args,omitempty"`   // ad-hoc
	StartTime     time.Time           `json:"start_time"`
	EndTime       time.Time           `json:"end_time"`
	ExitCode      int                 `json:"exit_code"`
	HostStats     map[string]string   `json:"host_stats,omitempty"` // host → status
}

// Duration returns the run duration as a human-readable string.
func (r *Record) Duration() string {
	d := r.EndTime.Sub(r.StartTime).Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// Result returns a short result string for display.
func (r *Record) Result() string {
	if r.ExitCode == 0 {
		return "ok"
	}
	return fmt.Sprintf("exit %d", r.ExitCode)
}

// ─── Storage ──────────────────────────────────────────────────────────────────

// Save writes a record into state with owner-only permissions.
func Save(r *Record) error {
	d := paths.Join(paths.StateDir(), "history")
	if d == "" {
		return fmt.Errorf("cannot save history: HOME is unset")
	}
	name := fmt.Sprintf("%s-%s", r.StartTime.Format("20060102-150405.000000000"), sanitize(r.PlaybookName))
	if r.ID != "" {
		name += "-" + sanitize(r.ID)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFile(filepath.Join(d, name+".json"), data)
}

// Load merges XDG and legacy records, newest first. XDG wins duplicate IDs.
// Missing directories are empty and reads never create them.
func Load() ([]*Record, error) {
	var records []*Record
	seen := map[string]bool{}
	for _, dir := range []string{paths.Join(paths.StateDir(), "history"), paths.Join(paths.LegacyDir(), "history")} {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			var record Record
			if err := json.Unmarshal(data, &record); err != nil {
				continue
			}
			key := record.ID
			if key == "" {
				key = strings.Join([]string{record.StartTime.Format(time.RFC3339Nano), record.Kind, record.PlaybookPath, record.Inventory, record.Module, record.Args}, "\x00")
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			records = append(records, &record)
		}
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].StartTime.After(records[j].StartTime) })
	return records, nil
}

// Limit returns the last n records.
func Limit(records []*Record, n int) []*Record {
	if n < 0 {
		return nil
	}
	if len(records) <= n {
		return records
	}
	return records[:n]
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for _, b := range []byte(s) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
			out = append(out, b)
		} else {
			out = append(out, '-')
		}
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return string(out)
}
