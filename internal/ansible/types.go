// Package ansible provides the shared, shell-free Ansible operations used by the
// CLI and terminal UI. Observations do not install or upgrade tools.
package ansible

import "time"

type ProjectContext struct {
	WorkDir     string `json:"workdir"`
	Inventory   string `json:"inventory,omitempty"`
	PlaybookDir string `json:"playbook_dir,omitempty"`
	Executable  string `json:"executable,omitempty"`
}

type RunRequest struct {
	Kind              string         `json:"kind"`
	Project           ProjectContext `json:"project"`
	Playbook          string         `json:"playbook,omitempty"`
	RolePath          string         `json:"role_path,omitempty"`
	Module            string         `json:"module,omitempty"`
	Args              string         `json:"-"`
	Hosts             string         `json:"hosts,omitempty"`
	Limit             string         `json:"limit,omitempty"`
	Tags              string         `json:"tags,omitempty"`
	Check             bool           `json:"check"`
	Diff              bool           `json:"diff"`
	Become            bool           `json:"become"`
	ExtraVars         []string       `json:"-"`
	VaultPasswordFile string         `json:"-"`
	Executable        string         `json:"executable,omitempty"`
	Env               []string       `json:"-"`
}

// Args and Env may contain secrets. Use RunPlan.Preview for display or JSON.
type CommandSpec struct {
	Executable string   `json:"executable"`
	Args       []string `json:"-"`
	Dir        string   `json:"dir"`
	Env        []string `json:"-"`
}

type RunPlan struct {
	Command        CommandSpec   `json:"command"`
	Preview        string        `json:"preview"`
	Request        RunRequest    `json:"request"`
	Runtime        RuntimeStatus `json:"runtime"`
	Callback       string        `json:"callback,omitempty"`
	Notes          []string      `json:"notes,omitempty"`
	roleContent    []byte
	runtimeOptions *RuntimeOptions
	installOptions *RuntimeOptions
	installOwner   string
}

type Event struct {
	Line   string `json:"line"`
	Stream string `json:"stream"`
}
type Result struct {
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration"`
}

type InventoryHost struct {
	Name   string         `json:"name"`
	Groups []string       `json:"groups"`
	Vars   map[string]any `json:"vars,omitempty"`
}
type InventoryGroup struct {
	Name     string         `json:"name"`
	Hosts    []string       `json:"hosts"`
	Children []string       `json:"children"`
	Vars     map[string]any `json:"vars,omitempty"`
}
type InventorySnapshot struct {
	Project    ProjectContext   `json:"project"`
	Hosts      []InventoryHost  `json:"hosts"`
	Groups     []InventoryGroup `json:"groups"`
	Warnings   string           `json:"warnings,omitempty"`
	ObservedAt time.Time        `json:"observed_at"`
}
type ConfigEntry struct {
	Name   string `json:"name"`
	Value  any    `json:"value"`
	Origin string `json:"origin,omitempty"`
	Type   string `json:"type,omitempty"`
}
type ConfigSnapshot struct {
	Project    ProjectContext `json:"project"`
	Entries    []ConfigEntry  `json:"entries"`
	Warnings   string         `json:"warnings,omitempty"`
	ObservedAt time.Time      `json:"observed_at"`
}

type RuntimeOptions struct {
	WorkDir       string
	Provider      string
	Executable    string
	UVExecutable  string
	Package       string
	Python        string
	Constraint    string
	CacheDir      string
	CheckInterval time.Duration
}
type RuntimeStatus struct {
	Executable         string `json:"executable"`
	PlaybookExecutable string `json:"playbook_executable,omitempty"`
	CoreVersion        string `json:"core_version,omitempty"`
	Python             string `json:"python,omitempty"`
	PythonVersion      string `json:"python_version,omitempty"`
	UVExecutable       string `json:"uv_executable,omitempty"`
	UVVersion          string `json:"uv_version,omitempty"`
	Managed            bool   `json:"managed"`
	Ownership          string `json:"ownership"` // uv, external, or unknown
	ToolPackage        string `json:"tool_package,omitempty"`
	ToolVersion        string `json:"tool_version,omitempty"`
	Constraint         string `json:"constraint,omitempty"`
	ToolDir            string `json:"tool_dir,omitempty"`
	Issue              string `json:"issue,omitempty"`
}
type UpdateStatus struct {
	Runtime       RuntimeStatus `json:"runtime"`
	State         string        `json:"state"` // current, available, unknown, or unmanaged
	LatestVersion string        `json:"latest_version,omitempty"`
	CheckedAt     time.Time     `json:"checked_at,omitempty"`
	LastAttempt   time.Time     `json:"last_attempt,omitempty"`
	Cached        bool          `json:"cached"`
	Stale         bool          `json:"stale"`
	Error         string        `json:"error,omitempty"`
	Source        string        `json:"source,omitempty"`
}
