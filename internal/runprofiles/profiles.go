// Package runprofiles persists named run configurations.
package runprofiles

import (
	"encoding/json"
	"os"
	"time"

	"github.com/daviddwlee84/lazyansible/internal/paths"
)

// Profile stores a complete run configuration that can be recalled quickly.
type Profile struct {
	Name      string    `json:"name"`
	WorkDir   string    `json:"work_dir,omitempty"`
	Playbook  string    `json:"playbook"`   // path or name
	Limit     string    `json:"limit"`      // --limit value
	Tags      []string  `json:"tags"`       // selected tags
	ExtraVars string    `json:"extra_vars"` // --extra-vars string
	CheckMode bool      `json:"check_mode"` // --check
	DiffMode  bool      `json:"diff_mode"`  // --diff
	Inventory string    `json:"inventory"`  // inventory path override
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func storePath() string { return paths.Join(paths.ConfigDir(), "run-profiles.json") }

// Load reads all profiles from disk.
func Load() ([]Profile, error) {
	data, err := paths.ReadConfigFile("run-profiles.json")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles []Profile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

// Save writes all profiles to disk, creating the directory if needed.
func Save(profiles []Profile) error {
	p := storePath()
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFile(p, data)
}

// Upsert adds or replaces a profile by name.
func Upsert(profiles []Profile, p Profile) []Profile {
	now := time.Now()
	for i, existing := range profiles {
		if existing.Name == p.Name {
			p.CreatedAt = existing.CreatedAt
			p.UpdatedAt = now
			profiles[i] = p
			return profiles
		}
	}
	p.CreatedAt = now
	p.UpdatedAt = now
	return append(profiles, p)
}

// Delete removes a profile by name.
func Delete(profiles []Profile, name string) []Profile {
	out := profiles[:0]
	for _, p := range profiles {
		if p.Name != name {
			out = append(out, p)
		}
	}
	return out
}
