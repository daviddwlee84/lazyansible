// Package ssh manages named SSH connection profiles for Ansible.
package ssh

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/daviddwlee84/lazyansible/internal/paths"
)

// Profile holds the SSH connection parameters for a named profile.
type Profile struct {
	Name        string `json:"name"`
	User        string `json:"user,omitempty"`
	KeyFile     string `json:"key_file,omitempty"`
	Port        int    `json:"port,omitempty"`
	BastionHost string `json:"bastion_host,omitempty"`
	BastionUser string `json:"bastion_user,omitempty"`
	ExtraArgs   string `json:"extra_args,omitempty"`
}

// ToExtraVarsRaw returns the profile as a space-separated key=val string
// suitable for passing as ansible --extra-vars.
func (p *Profile) ToExtraVarsRaw() string {
	var parts []string
	if p.User != "" {
		parts = append(parts, "ansible_user="+p.User)
	}
	if p.KeyFile != "" {
		parts = append(parts, "ansible_ssh_private_key_file="+p.KeyFile)
	}
	if p.Port > 0 && p.Port != 22 {
		parts = append(parts, fmt.Sprintf("ansible_port=%d", p.Port))
	}
	if p.BastionHost != "" {
		sshArgs := fmt.Sprintf(`ansible_ssh_common_args="-o ProxyJump=%s"`, p.BastionHost)
		if p.BastionUser != "" {
			sshArgs = fmt.Sprintf(`ansible_ssh_common_args="-o ProxyJump=%s@%s"`, p.BastionUser, p.BastionHost)
		}
		parts = append(parts, sshArgs)
	} else if p.ExtraArgs != "" {
		parts = append(parts, fmt.Sprintf(`ansible_ssh_common_args="%s"`, p.ExtraArgs))
	}
	return strings.Join(parts, " ")
}

// Summary returns a short one-line description for the list view.
func (p *Profile) Summary() string {
	parts := []string{p.Name}
	if p.User != "" {
		parts = append(parts, p.User)
	}
	port := 22
	if p.Port > 0 {
		port = p.Port
	}
	parts = append(parts, fmt.Sprintf(":%d", port))
	if p.KeyFile != "" {
		parts = append(parts, shortenPath(p.KeyFile))
	}
	if p.BastionHost != "" {
		parts = append(parts, "via "+p.BastionHost)
	}
	return strings.Join(parts, "  ")
}

// ─── Storage ──────────────────────────────────────────────────────────────────

func profilesPath() (string, error) {
	p := paths.Join(paths.ConfigDir(), "ssh-profiles.json")
	if p == "" {
		return "", fmt.Errorf("cannot locate config directory: HOME is unset")
	}
	return p, nil
}

// Load reads profiles from disk. Returns an empty slice if the file doesn't exist.
func Load() ([]*Profile, error) {
	data, err := paths.ReadConfigFile("ssh-profiles.json")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles []*Profile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

// Save writes profiles to disk.
func Save(profiles []*Profile) error {
	path, err := profilesPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFile(path, data)
}

func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return strings.Replace(p, home, "~", 1)
}
