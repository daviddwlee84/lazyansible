// Package ui contains the top-level Bubble Tea application model.
package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/buildinfo"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/editor"
	"github.com/daviddwlee84/lazyansible/internal/galaxy"
	"github.com/daviddwlee84/lazyansible/internal/history"
	"github.com/daviddwlee84/lazyansible/internal/inventory"
	"github.com/daviddwlee84/lazyansible/internal/notify"
	"github.com/daviddwlee84/lazyansible/internal/runner"
	"github.com/daviddwlee84/lazyansible/internal/runprofiles"
	"github.com/daviddwlee84/lazyansible/internal/ui/panels"
	"github.com/daviddwlee84/lazyansible/internal/vault"
)

// AppMode tracks which overlay (if any) is currently shown.
type AppMode int

const (
	AppModeNormal AppMode = iota
	AppModeAdHoc
	AppModeExtraVars
	AppModeTagsBrowser
	AppModeVault
	AppModeHistory
	AppModeRoles
	AppModeEnvSwitch
	AppModeSSHProfile
	AppModeHelp
	AppModeGalaxy
	AppModeRunProfiles
	AppModePlaybookViewer
	AppModeWorkbench
	AppModeRunReview
	AppModePalette
)

// Config holds the launch-time configuration.
type Config struct {
	Context          context.Context
	InventoryPath    string
	PlaybookDir      string
	WorkDir          string
	DefaultCheckMode bool
	DefaultDiffMode  bool
	ConfigPath       string
	CheckUpdates     bool
	Runtime          ansible.RuntimeOptions
	generation       uint64
}

// App is the root Bubble Tea model.
type App struct {
	config    Config
	program   *tea.Program
	ctx       context.Context
	cancelRun context.CancelFunc
	cancelAll context.CancelFunc

	width  int
	height int

	// Workbench and asynchronous request ownership.
	workbench             *workbenchModel
	palette               *paletteModel
	review                runReview
	reviewID              uint64
	runtimeBusy           bool
	runtimePending        bool
	runtimeID             uint64
	runtimeCancel         context.CancelFunc
	profileNeedsSelection bool
	runtimeSummary        string
	pendingProfile        *runprofiles.Profile

	// Panel models.
	invPanel    *panels.InventoryPanel
	pbPanel     *panels.PlaybooksPanel
	statusPanel *panels.StatusPanel
	logsPanel   *panels.LogsPanel

	focused    core.Panel
	mode       AppMode
	helpOffset int

	// v0.2 overlays.
	adhocOverlay     *AdHocOverlay
	extraVarsOverlay *ExtraVarsOverlay
	tagsOverlay      *TagsOverlay

	// v0.3 overlays.
	vaultOverlay   *VaultOverlay
	historyOverlay *HistoryOverlay

	// v0.4 overlays.
	rolesOverlay      *RolesOverlay
	envSwitchOverlay  *EnvSwitchOverlay
	sshProfileOverlay *SSHProfileOverlay

	// v0.6 overlays.
	galaxyOverlay      *GalaxyOverlay
	runProfilesOverlay *RunProfilesOverlay

	// v0.7 overlays.
	pbViewerOverlay *PlaybookViewerOverlay

	// Run state.
	inventory    *core.Inventory
	playbooks    []*core.Playbook
	running      bool
	quitting     bool
	statusMsg    string
	extraVarsRaw string

	// v0.3 state.
	vaultPassword     string          // current vault password (cleared after run)
	vaultPasswordFile string          // temp file path (cleaned up after run)
	retryHosts        []string        // failed hosts from last run, for retry
	runRecord         *history.Record // in-progress record, saved on finish

	// v0.4 state.
	sshExtraVars   string // applied SSH profile extra-vars
	tempPlaybook   string // temp role-runner playbook (cleaned up after run)
	logsFullscreen bool   // Z toggles logs to full height

	// v0.5 state.
	linting    bool   // ansible-lint run in progress
	lastExport string // path of last Markdown export

	// v0.6 state (nothing extra — overlays are self-contained)

	// v0.7 state.
	notifyOnFinish bool // send desktop notification when run completes
}

// New creates a new App with the given configuration.
func New(cfg Config) *App {
	parent := cfg.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	cfg.generation = 1
	cfg.Runtime.WorkDir = cfg.WorkDir
	galaxy.SetContext(ansible.ProjectContext{WorkDir: cfg.WorkDir, Inventory: cfg.InventoryPath, PlaybookDir: cfg.PlaybookDir, Executable: cfg.Runtime.Executable}, cfg.Runtime)
	a := &App{
		config:  cfg,
		focused: core.PanelInventory,
		mode:    AppModeNormal,
		ctx:     ctx, cancelAll: cancel,
	}

	a.invPanel = panels.NewInventoryPanel(nil, 0, 0)
	a.pbPanel = panels.NewPlaybooksPanel(nil, 0, 0)
	a.statusPanel = panels.NewStatusPanel(0, 0)
	a.logsPanel = panels.NewLogsPanel(0, 0)

	a.adhocOverlay = newAdHocOverlay(0, 0)
	a.extraVarsOverlay = newExtraVarsOverlay(0, 0)
	a.tagsOverlay = newTagsOverlay(0, 0)
	a.vaultOverlay = newVaultOverlay(0, 0)
	a.historyOverlay = newHistoryOverlay(0, 0)
	a.rolesOverlay = newRolesOverlay(0, 0)
	a.envSwitchOverlay = newEnvSwitchOverlay(0, 0)
	a.sshProfileOverlay = newSSHProfileOverlay(0, 0)
	a.galaxyOverlay = newGalaxyOverlay(0, 0)
	a.runProfilesOverlay = newRunProfilesOverlay(0, 0)
	a.pbViewerOverlay = newPlaybookViewerOverlay(0, 0)

	a.pbPanel.SetCheckMode(cfg.DefaultCheckMode)
	a.pbPanel.SetDiffMode(cfg.DefaultDiffMode)
	a.workbench = newWorkbench()
	a.palette = newPalette()
	a.updateFocus()
	return a
}

func (a *App) SetProgram(p *tea.Program) { a.program = p }

// Close cancels work owned by this dashboard, including pending discovery.
func (a *App) Close() { a.cancelAll() }

// SetNotifyOnFinish enables desktop notifications at run completion.
func (a *App) SetNotifyOnFinish(v bool) { a.notifyOnFinish = v }

func (a *App) Init() tea.Cmd {
	return tea.Batch(
		loadInventoryCmd(a.config),
		loadPlaybooksCmd(a.config),
		a.scanVaultCmd(),
		a.runtimeRefreshCmd(a.config.CheckUpdates, false),
	)
}

// ShutdownMsg requests cancellation and waits for an owned child to stop.
type ShutdownMsg struct{}

// ─── Messages ────────────────────────────────────────────────────────────────

type inventoryLoadedMsg struct {
	inv        *core.Inventory
	path       string // absolute path passed to ansible -i (set after auto-discovery)
	generation uint64
}
type playbooksLoadedMsg struct {
	pbs        []*core.Playbook
	generation uint64
}
type vaultScanDoneMsg struct{ hasVault bool }
type lintFinishedMsg struct{ exitCode int }
type exportDoneMsg struct {
	path string
	err  error
}
type errMsg struct {
	err        error
	generation uint64
}

// ─── Update ──────────────────────────────────────────────────────────────────

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(ShutdownMsg); ok {
		return a, a.requestQuit()
	}
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		a.width = sz.Width
		a.height = sz.Height
		a.resizePanels()
		return a, nil
	}

	// Mouse events: only handle left-click for panel focus; discard everything
	// else (motion, scroll, hover) so mouse movement never triggers a full
	// rerender and never reaches text-input overlays (which would cause lag).
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			if a.mode == AppModeNormal {
				a.handleMouseClick(mouse.X, mouse.Y)
			}
		}
		return a, nil
	}

	// Editor done: reload content and show status.
	if ed, ok := msg.(editor.DoneMsg); ok {
		if ed.Err != nil {
			a.statusMsg = "Editor error: " + ed.Err.Error()
		} else {
			// Reload the viewer if it's open and still shows the same file.
			a.pbViewerOverlay.Reload()
			a.statusMsg = fmt.Sprintf("Saved: %s", ed.Path)
		}
		return a, nil
	}

	// Two-step open: resolved path → launch editor.
	if ep, ok := msg.(editorOpenPathMsg); ok {
		return a, editor.Open(ep.path)
	}

	if cmd, handled := a.updateWorkbenchResult(msg); handled {
		return a, cmd
	}

	switch msg := msg.(type) {

	case inventoryLoadedMsg:
		if msg.generation != a.config.generation {
			return a, nil
		}
		a.inventory = msg.inv
		a.invPanel.SetInventory(msg.inv)
		if msg.path != "" {
			a.config.InventoryPath = msg.path
			a.envSwitchOverlay.Scan(a.config.WorkDir, a.config.InventoryPath)
		}
		a.statusMsg = fmt.Sprintf("Inventory: %d hosts, %d groups",
			len(msg.inv.Hosts), len(msg.inv.Groups))

	case playbooksLoadedMsg:
		if msg.generation != a.config.generation {
			return a, nil
		}
		a.playbooks = msg.pbs
		a.pbPanel.SetPlaybooks(msg.pbs)
		a.statusMsg = fmt.Sprintf("Found %d playbooks", len(msg.pbs))
		if a.pendingProfile != nil {
			p := *a.pendingProfile
			a.pendingProfile = nil
			a.selectProfilePlaybook(p)
		}

	case vaultScanDoneMsg:
		if msg.hasVault {
			a.statusMsg = "⚠ Vault-encrypted files detected — press V to set password"
		}

	case runner.LogMsg:
		a.logsPanel.AddLine(msg.Line)

	case runner.HostStatusMsg:
		a.statusPanel.UpdateHost(msg.Host, msg.Status, msg.Task)

	case runner.RunFinishedMsg:
		return a, a.handleRunFinished(msg)

	case lintFinishedMsg:
		a.linting = false
		if a.quitting {
			return a, tea.Quit
		}
		if msg.exitCode == 0 {
			a.statusMsg = "ansible-lint: no issues found ✓"
		} else {
			a.statusMsg = fmt.Sprintf("ansible-lint: issues found (exit %d) — see logs", msg.exitCode)
		}

	case exportDoneMsg:
		if msg.err != nil {
			a.statusMsg = "Export failed: " + msg.err.Error()
		} else {
			a.lastExport = msg.path
			a.statusMsg = "Exported → " + msg.path
		}

	case panels.RunRequestMsg:
		return a, a.startRun(msg)

	case EnvSwitchMsg:
		a.mode = AppModeNormal
		return a, a.switchInventory(msg.Path)

	case SSHProfileAppliedMsg:
		a.sshExtraVars = msg.ExtraVars
		if msg.ExtraVars != "" {
			a.statusMsg = "SSH profile applied (will be used on next run)"
		} else {
			a.statusMsg = "SSH profile cleared"
		}
		a.mode = AppModeNormal
		return a, nil

	case galaxyLoadedMsg:
		return a, a.galaxyOverlay.Update(msg)

	case galaxyInstallDoneMsg:
		return a, a.galaxyOverlay.Update(msg)

	case RunProfileLoadMsg:
		a.mode = AppModeNormal
		return a, a.applyRunProfile(msg.Profile)
	case panels.InspectInventoryMsg:
		target := "group:" + msg.Group
		if msg.Host != "" {
			target = "host:" + msg.Host
		}
		return a, a.openWorkbench("inventory", target)
	case panels.SetLimitMsg:
		a.pbPanel.SetLimit(msg.Limit)
		a.statusMsg = "Limit → " + msg.Limit
		return a, nil
	case panels.ViewPlaybookMsg:
		if msg.Playbook != nil {
			a.pbViewerOverlay.Load(msg.Playbook.Name, msg.Playbook.Path)
			a.mode = AppModePlaybookViewer
		}
		return a, nil
	case AdHocRunMsg:
		return a, a.startAdHoc(msg.Opts)
	case RoleRunMsg:
		return a, a.startRoleRun(msg)
	case HistoryRunMsg:
		return a, a.startRunFromHistory(msg.Record)
	case ExtraVarsConfirmedMsg:
		a.extraVarsRaw = msg.Raw
		a.pbPanel.SetExtraVars(msg.Raw)
		a.statusMsg = "Extra vars updated (values hidden)"
		a.mode = AppModeNormal
		return a, nil
	case TagsConfirmedMsg:
		a.pbPanel.SetActiveTags(msg.Tags)
		a.statusMsg = "Tags updated"
		a.mode = AppModeNormal
		return a, nil
	case VaultPasswordMsg:
		a.vaultPassword = msg.Password
		a.statusMsg = "Vault password updated for this session"
		a.mode = AppModeNormal
		return a, nil
	case pbViewerCloseMsg:
		a.mode = AppModeNormal
		return a, nil

	case errMsg:
		if msg.generation != 0 && msg.generation != a.config.generation {
			return a, nil
		}
		a.statusMsg = "Error: " + msg.err.Error()

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, a.requestQuit()
		}
		if a.mode != AppModeNormal {
			return a.updateOverlay(msg)
		}
		return a.updateNormalKeys(msg)
	}
	if a.mode != AppModeNormal {
		return a.updateOverlay(msg)
	}
	return a, a.delegateToPanel(msg)
}

func (a *App) updateLegacyKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When the log panel's search bar is active, forward ALL key events to it
	// so global shortcuts (q, V, etc.) don't fire during text input.
	if (a.focused == core.PanelLogs && a.logsPanel.SearchActive()) ||
		(a.focused == core.PanelInventory && a.invPanel.FilterActive()) ||
		(a.focused == core.PanelPlaybooks && a.pbPanel.FilterActive()) {
		return a, a.delegateToPanel(msg)
	}

	if msg.String() == "N" && a.focused == core.PanelLogs {
		return a, a.delegateToPanel(msg)
	}
	if a.focused != core.PanelInventory && (msg.String() == "h" || msg.String() == "left" || msg.String() == "l" || msg.String() == "right") {
		if msg.String() == "h" || msg.String() == "left" {
			a.cycleFocus(-1)
		} else {
			a.cycleFocus(1)
		}
		return a, nil
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return a, a.requestQuit()

	case "?":
		a.helpOffset = 0
		a.mode = AppModeHelp
		return a, nil

	case "tab":
		a.cycleFocus(1)
		return a, nil
	case "shift+tab":
		a.cycleFocus(-1)
		return a, nil

	case "1":
		a.focused = core.PanelInventory
		a.updateFocus()
	case "2":
		a.focused = core.PanelPlaybooks
		a.updateFocus()
	case "3":
		a.focused = core.PanelStatus
		a.updateFocus()
	case "4":
		a.focused = core.PanelLogs
		a.updateFocus()

	case "ctrl+l":
		a.logsPanel.Clear()

	case "Z":
		a.logsFullscreen = !a.logsFullscreen
		a.resizePanels()

	case "!":
		target := ""
		if a.focused == core.PanelInventory {
			if h := a.invPanel.SelectedHost(); h != "" {
				target = h
			} else if g := a.invPanel.SelectedGroup(); g != "" {
				target = g
			}
		}
		if target == "" {
			target = a.pbPanel.CurrentLimit()
		}
		a.adhocOverlay.SetTarget(target, a.config.InventoryPath)
		a.mode = AppModeAdHoc
		return a, nil

	case "e":
		if a.focused == core.PanelPlaybooks {
			a.extraVarsOverlay.SetCurrent(a.extraVarsRaw)
			a.mode = AppModeExtraVars
			return a, nil
		}

	case "t":
		if a.focused == core.PanelPlaybooks {
			if pb := a.pbPanel.SelectedPlaybook(); pb != nil {
				a.tagsOverlay.SetTags(pb.Tags)
				a.tagsOverlay.SetSelectedTags(strings.Join(a.pbPanel.SelectedTags(), ","))
				a.mode = AppModeTagsBrowser
				return a, nil
			}
		}

	// ── v0.3 overlays ─────────────────────────────────────────────────────

	case "V":
		a.vaultOverlay.Reset()
		a.mode = AppModeVault
		return a, nil

	case "H":
		a.historyOverlay.Reload()
		a.mode = AppModeHistory
		return a, nil

	case "R":
		if len(a.retryHosts) > 0 && !a.running {
			limit := strings.Join(a.retryHosts, ",")
			a.pbPanel.SetLimit(limit)
			a.statusMsg = fmt.Sprintf("Retry limit set: %s", limit)
		} else if a.running {
			a.statusMsg = "Cannot retry while a run is in progress"
		} else {
			a.statusMsg = "No failed hosts to retry"
		}

	// ── v0.4 overlays ─────────────────────────────────────────────────────

	case "O":
		// Role browser.
		rolesDir := filepath.Join(a.config.WorkDir, "roles")
		limit := a.pbPanel.CurrentLimit()
		a.rolesOverlay.Load(rolesDir, a.config.InventoryPath, limit)
		a.mode = AppModeRoles
		return a, nil

	case "N":
		// Environment / inventory switcher.
		a.envSwitchOverlay.Scan(a.config.WorkDir, a.config.InventoryPath)
		a.mode = AppModeEnvSwitch
		return a, nil

	case "P":
		// SSH profile manager.
		a.sshProfileOverlay.loadProfiles()
		a.mode = AppModeSSHProfile
		return a, nil

	case "L":
		// Ansible-lint on the selected playbook.
		if a.running || a.linting {
			a.statusMsg = "Cannot lint while a run is in progress"
			return a, nil
		}
		if pb := a.pbPanel.SelectedPlaybook(); pb != nil {
			if err := runner.CheckLintBinary(); err != nil {
				a.statusMsg = err.Error()
				return a, nil
			}
			a.linting = true
			a.logsPanel.Clear()
			a.statusMsg = fmt.Sprintf("Linting %s…", pb.Name)
			ctx, cancel := context.WithCancel(a.ctx)
			a.cancelRun = cancel
			sendFn := func(m tea.Msg) {
				if a.program != nil {
					a.program.Send(m)
				}
			}
			project := a.projectContext()
			return a, func() tea.Msg {
				plan, err := ansible.Prepare(ctx, ansible.RunRequest{Kind: "lint", Project: project, Playbook: pb.Path})
				if err != nil {
					return lintFinishedMsg{exitCode: -1}
				}
				msg := runner.StreamPlanCmd(ctx, plan, sendFn)()
				if rf, ok := msg.(runner.RunFinishedMsg); ok {
					return lintFinishedMsg{exitCode: rf.ExitCode}
				}
				return lintFinishedMsg{exitCode: -1}
			}
		}
		a.statusMsg = "No playbook selected"

	case "X":
		// Export run summary as Markdown.
		lines := a.logsPanel.Lines()
		if len(lines) == 0 {
			a.statusMsg = "Nothing to export yet"
			return a, nil
		}
		rec := a.runRecord // may be nil if run already finished
		workDir := a.config.WorkDir
		return a, func() tea.Msg {
			path, err := exportRunMarkdown(workDir, rec, lines)
			return exportDoneMsg{path: path, err: err}
		}

	// ── v0.7 features ─────────────────────────────────────────────────────

	case "I":
		// Live reload inventory + playbooks without restarting.
		a.statusMsg = "Reloading inventory and playbooks…"
		return a, a.reloadProject()

	case " ":
		// Playbook viewer: show YAML source of selected playbook.
		if a.focused == core.PanelPlaybooks {
			if pb := a.pbPanel.SelectedPlaybook(); pb != nil {
				a.pbViewerOverlay.Load(pb.Name, pb.Path)
				a.mode = AppModePlaybookViewer
				return a, nil
			}
		}

	case "E":
		// Open selected playbook directly in $EDITOR (skip viewer overlay).
		if a.focused == core.PanelPlaybooks {
			if pb := a.pbPanel.SelectedPlaybook(); pb != nil {
				return a, editor.Open(pb.Path)
			}
		}
		// Also allow editing from inventory: open host_vars / group_vars file.
		if a.focused == core.PanelInventory {
			if host := a.invPanel.SelectedHost(); host != "" {
				return a, editorOpenVarsFile(inventoryBaseDir(a.config), "host_vars", host)
			} else if group := a.invPanel.SelectedGroup(); group != "" {
				return a, editorOpenVarsFile(inventoryBaseDir(a.config), "group_vars", group)
			}
		}

	// ── v0.6 overlays ─────────────────────────────────────────────────────

	case "A":
		// Ansible Galaxy browser.
		a.mode = AppModeGalaxy
		galaxy.SetContext(a.projectContext(), a.config.Runtime)
		return a, a.galaxyOverlay.Load()

	case "F":
		// Run profiles.
		a.runProfilesOverlay.reload()
		// Snapshot current state for save.
		pb := a.pbPanel.SelectedPlaybook()
		pbName := ""
		if pb != nil {
			pbName = pb.Path
		}
		a.runProfilesOverlay.SetSnapshot(
			pbName,
			a.pbPanel.CurrentLimit(),
			a.pbPanel.SelectedTags(),
			a.extraVarsRaw,
			a.pbPanel.CheckMode(),
			a.pbPanel.DiffMode(),
			a.config.InventoryPath,
			a.config.WorkDir,
		)
		a.mode = AppModeRunProfiles
		return a, nil

	}

	return a, a.delegateToPanel(msg)
}

func (a *App) updateOverlay(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.mode == AppModeWorkbench {
		return a, a.updateWorkbench(msg)
	}
	if a.mode == AppModeRunReview {
		return a, a.updateReview(msg)
	}
	if a.mode == AppModePalette {
		return a, a.updatePalette(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
		consumed := false
		switch a.mode {
		case AppModeTagsBrowser:
			consumed = a.tagsOverlay.HandleEscape()
		case AppModeHistory:
			consumed = a.historyOverlay.HandleEscape()
		case AppModeRoles:
			consumed = a.rolesOverlay.HandleEscape()
		case AppModeGalaxy:
			consumed = a.galaxyOverlay.HandleEscape()
		case AppModeRunProfiles:
			consumed = a.runProfilesOverlay.HandleEscape()
		case AppModeSSHProfile:
			consumed = a.sshProfileOverlay.HandleEscape()
		}
		if !consumed {
			a.mode = AppModeNormal
		}
		return a, nil
	}
	switch a.mode {
	case AppModeHelp:
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "j", "down":
				a.helpOffset++
			case "k", "up":
				a.helpOffset = max(0, a.helpOffset-1)
			case "g", "home":
				a.helpOffset = 0
			case "G", "end":
				a.helpOffset = len(a.actions())
			case "q":
				a.mode = AppModeNormal
			}
		}
		return a, nil
	case AppModeAdHoc:
		return a, a.adhocOverlay.Update(msg)
	case AppModeExtraVars:
		return a, a.extraVarsOverlay.Update(msg)
	case AppModeTagsBrowser:
		return a, a.tagsOverlay.Update(msg)
	case AppModeVault:
		return a, a.vaultOverlay.Update(msg)
	case AppModeHistory:
		return a, a.historyOverlay.Update(msg)
	case AppModeRoles:
		return a, a.rolesOverlay.Update(msg)
	case AppModeEnvSwitch:
		return a, a.envSwitchOverlay.Update(msg)
	case AppModeSSHProfile:
		return a, a.sshProfileOverlay.Update(msg)
	case AppModeGalaxy:
		return a, a.galaxyOverlay.Update(msg)
	case AppModeRunProfiles:
		return a, a.runProfilesOverlay.Update(msg)
	case AppModePlaybookViewer:
		return a, a.pbViewerOverlay.Update(msg)
	}
	return a, nil
}

func (a *App) delegateToPanel(msg tea.Msg) tea.Cmd {
	switch a.focused {
	case core.PanelInventory:
		return a.invPanel.Update(msg)
	case core.PanelPlaybooks:
		old := a.pbPanel.SelectedPlaybook()
		cmd := a.pbPanel.Update(msg)
		selected := a.pbPanel.SelectedPlaybook()
		if a.pendingProfile == nil && selected != nil && (old == nil || old.Path != selected.Path) {
			a.profileNeedsSelection = false
		}
		return cmd
	case core.PanelStatus:
		return a.statusPanel.Update(msg)
	case core.PanelLogs:
		return a.logsPanel.Update(msg)
	}
	return nil
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (a *App) View() string {
	if a.width == 0 {
		return "Initializing…"
	}

	switch a.mode {
	case AppModeWorkbench:
		return a.workbenchView()
	case AppModeRunReview:
		return a.reviewView()
	case AppModePalette:
		return a.paletteView()
	case AppModeHelp:
		return a.renderOverlay(a.helpContent())
	case AppModeAdHoc:
		return a.renderOverlay(a.adhocOverlay.View())
	case AppModeExtraVars:
		return a.renderOverlay(a.extraVarsOverlay.View())
	case AppModeTagsBrowser:
		return a.renderOverlay(a.tagsOverlay.View())
	case AppModeVault:
		return a.renderOverlay(a.vaultOverlay.View())
	case AppModeHistory:
		return a.renderOverlay(a.historyOverlay.View())
	case AppModeRoles:
		return a.renderOverlay(a.rolesOverlay.View())
	case AppModeEnvSwitch:
		return a.renderOverlay(a.envSwitchOverlay.View())
	case AppModeSSHProfile:
		return a.renderOverlay(a.sshProfileOverlay.View())
	case AppModeGalaxy:
		return a.renderOverlay(a.galaxyOverlay.View())
	case AppModeRunProfiles:
		return a.renderOverlay(a.runProfilesOverlay.View())
	case AppModePlaybookViewer:
		return a.renderOverlay(a.pbViewerOverlay.View())
	}

	return a.baseView()
}

// topPanelHeight is the fixed row count reserved for the inventory/playbooks/status row.
// Keeping this constant avoids any dynamic arithmetic that could introduce off-by-one
// errors when log lines stream in. Adjust if you want more/less space at the top.
const topPanelHeight = 14

func (a *App) baseView() string {
	if a.width < 100 || a.height < 22 {
		header := fmt.Sprintf("lazyansible %s · %s", buildinfo.String(), a.runtimeSummary)
		views := []string{a.invPanel.View(), a.pbPanel.View(), a.statusPanel.View(), a.logsPanel.View()}
		titles := []string{"1 Inventory (static preview)", "2 Playbooks", "3 Status", "4 Logs"}
		content := titles[int(a.focused)] + "\n" + views[int(a.focused)]
		body := fitScreen(content, max(1, a.width), max(1, a.height-4))
		state := a.statusMsg
		if a.running {
			state = "RUNNING · " + state
		}
		if a.runtimeBusy {
			state = "UPDATING ANSIBLE · " + state
		}
		return fitScreen(header+"\n"+body+"\n"+fitScreen(state, max(1, a.width), 1)+"\n"+a.renderStatusBar(), max(1, a.width), max(1, a.height))
	}
	// Layout: header(1) + topRow(topPanelHeight) + logsBox(rest) + statusBar(1) = a.height
	available := a.height - 3 // header, feedback, and shortcuts
	header := strings.TrimRight(a.renderHeader(), "\n")
	statusBar := strings.TrimRight(a.renderStatusBar(), "\n")
	feedback := fitScreen(a.statusMsg, max(1, a.width), 1)

	var logsView string
	if a.logsFullscreen {
		logsView = strings.TrimRight(
			a.wrapPanel(a.logsPanel.View(), a.width, available, true, "Logs"), "\n")
		return fitScreen(forceHeight(header+"\n"+logsView+"\n"+feedback+"\n"+statusBar, a.height), a.width, a.height)
	}

	// Top row gets a fixed height; logs get everything else.
	topH := topPanelHeight
	if topH > available-4 {
		topH = available - 4 // guarantee at least 4 rows for logs
	}
	botH := available - topH

	invW := a.width / 4
	pbW := a.width / 4
	statusW := a.width - invW - pbW

	invView := strings.TrimRight(
		a.wrapPanel(a.invPanel.View(), invW, topH, a.focused == core.PanelInventory, "Inventory (static)"), "\n")
	pbView := strings.TrimRight(
		a.wrapPanel(a.pbPanel.View(), pbW, topH, a.focused == core.PanelPlaybooks, "Playbooks"), "\n")
	statusView := strings.TrimRight(
		a.wrapPanel(a.statusPanel.View(), statusW, topH, a.focused == core.PanelStatus, "Status"), "\n")
	topRow := strings.TrimRight(
		lipgloss.JoinHorizontal(lipgloss.Top, invView, pbView, statusView), "\n")
	logsView = strings.TrimRight(
		a.wrapPanel(a.logsPanel.View(), a.width, botH, a.focused == core.PanelLogs, "Logs"), "\n")

	// Assemble and guarantee exactly a.height rows so alt-screen never scrolls.
	return fitScreen(forceHeight(header+"\n"+topRow+"\n"+logsView+"\n"+feedback+"\n"+statusBar, a.height), a.width, a.height)
}

func (a *App) renderOverlay(content string) string {
	return fitScreen(lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, fitScreen(content, max(1, a.width), max(1, a.height))), max(1, a.width), max(1, a.height))
}

// wrapPanel wraps content in a bordered panel box of exactly w×h terminal cells.
// title is injected into the top border line (e.g. "╭─ Inventory ───╮").
func (a *App) wrapPanel(content string, w, h int, focused bool, title string) string {
	// Inner dimensions: width-4 (border 2 + padding 2), height-2 (border top+bottom).
	innerW := w - 4
	innerH := h - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	content = clipLines(content, innerH)

	style := panelStyle.Width(innerW).Height(innerH)
	if focused {
		style = panelFocusedStyle.Width(innerW).Height(innerH)
	}
	rendered := style.Render(content)

	// Inject the panel title into the top border line.
	if title != "" {
		rendered = injectBorderTitle(rendered, title, focused)
	}
	return rendered
}

// injectBorderTitle replaces the start of the top border dash-run with the title text.
// Input:  ╭──────────────────────────────╮
// Output: ╭─ Inventory ─────────────────╮
func injectBorderTitle(box, title string, focused bool) string {
	lines := strings.SplitN(box, "\n", 2)
	if len(lines) == 0 {
		return box
	}
	topLine := lines[0]

	// Strip ANSI codes to measure and find the dash run.
	plain := stripANSI(topLine)
	// The rounded top border starts with ╭ (3 bytes) followed by ─ runes.
	// We want to replace "╭─" with "╭─ Title ─".
	titleText := " " + title + " "

	// Find position of first ─ in the plain string.
	dashStart := strings.Index(plain, "─")
	if dashStart < 0 {
		return box // not a bordered box we recognise
	}

	// Build the replacement top line by working on the visual plain text,
	// then re-applying the border colour.
	var titleStyled string
	if focused {
		titleStyled = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7C3AED")).Bold(true).Render(titleText)
	} else {
		titleStyled = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")).Render(titleText)
	}

	// The corner ╭ in the plain string is 3 bytes wide (UTF-8).
	// Replace "╭─" with "╭─<titleStyled>".
	// We need the total visual width to stay the same, so we count how many
	// dashes the title consumes and remove them from the plain run.
	titleVisualW := lipgloss.Width(titleStyled)
	// Count how many ─ runes are in the original plain top border.
	totalDashes := strings.Count(plain, "─")
	// We emit: ╭ + ─ (1 explicit) + titleStyled + remainingDashes + ╮
	// To match original width (corners + totalDashes) we need:
	//   1 + 1 + titleVisualW + remainingDashes + 1 == 2 + totalDashes
	//   remainingDashes = totalDashes - titleVisualW - 1
	remainingDashes := totalDashes - titleVisualW - 1
	if remainingDashes < 1 {
		// Title too wide to fit; skip injection.
		return box
	}

	// Rebuild the border colour.
	borderColor := colorBorder
	if focused {
		borderColor = colorBorderFocus
	}
	cornerStyle := lipgloss.NewStyle().Foreground(borderColor)
	dashStyle := lipgloss.NewStyle().Foreground(borderColor)

	newTop := cornerStyle.Render("╭") +
		dashStyle.Render("─") +
		titleStyled +
		dashStyle.Render(strings.Repeat("─", remainingDashes)) +
		cornerStyle.Render("╮")

	if len(lines) == 1 {
		return newTop
	}
	return newTop + "\n" + lines[1]
}

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// clipLines truncates s to at most maxLines newline-separated lines.
func clipLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	count := 0
	for i, ch := range s {
		if ch == '\n' {
			count++
			if count >= maxLines {
				return s[:i]
			}
		}
	}
	return s
}

// forceHeight ensures the view string is EXACTLY h terminal rows.
//
// It always preserves the first line (header) and the last line (status bar).
// Any surplus lines are trimmed from the end of the middle section (logs).
// Any shortage is padded with empty lines before the status bar.
// This is the only reliable guard against Bubble Tea alt-screen scroll-off.
func forceHeight(view string, h int) string {
	if h <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	// Strip a single trailing empty element produced by a final "\n".
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	switch {
	case len(lines) == h:
		return strings.Join(lines, "\n")

	case len(lines) > h:
		// Too tall: remove excess lines from the bottom of the middle section
		// so that the header (lines[0]) and status bar (lines[last]) are kept.
		excess := len(lines) - h
		header := lines[0]
		statusBar := lines[len(lines)-1]
		middle := lines[1 : len(lines)-1]
		if excess >= len(middle) {
			middle = nil
		} else {
			middle = middle[:len(middle)-excess]
		}
		out := make([]string, 0, h)
		out = append(out, header)
		out = append(out, middle...)
		out = append(out, statusBar)
		return strings.Join(out, "\n")

	default:
		// Too short: pad with blank lines before the status bar.
		diff := h - len(lines)
		statusBar := lines[len(lines)-1]
		lines = lines[:len(lines)-1]
		for i := 0; i < diff; i++ {
			lines = append(lines, "")
		}
		lines = append(lines, statusBar)
		return strings.Join(lines, "\n")
	}
}

func (a *App) renderHeader() string {
	// ── Style atoms ───────────────────────────────────────────────────────
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#06B6D4")).Bold(true)
	verStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#334155"))
	sepStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1E293B"))
	badgeMuted := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748B"))

	// ── Logo section ──────────────────────────────────────────────────────
	logo := logoStyle.Render("⚡ lazyansible") +
		verStyle.Render(" "+buildinfo.String())

	// ── State badges ──────────────────────────────────────────────────────
	var badges []string
	if a.vaultPassword != "" {
		badges = append(badges, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).Bold(true).
			Background(lipgloss.Color("#1C1A0E")).
			Padding(0, 1).Render("🔐 vault"))
	}
	if a.sshExtraVars != "" {
		badges = append(badges, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22C55E")).Bold(true).
			Background(lipgloss.Color("#0A1A0E")).
			Padding(0, 1).Render("🔑 ssh"))
	}
	if len(a.retryHosts) > 0 {
		badges = append(badges, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")).Bold(true).
			Background(lipgloss.Color("#1A0A0A")).
			Padding(0, 1).Render(fmt.Sprintf("↺ %d failed", len(a.retryHosts))))
	}
	if a.running {
		badges = append(badges, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#111827")).Bold(true).
			Background(lipgloss.Color("#06B6D4")).
			Padding(0, 1).Render("▶ RUNNING"))
	} else if a.linting {
		badges = append(badges, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#111827")).Bold(true).
			Background(lipgloss.Color("#F59E0B")).
			Padding(0, 1).Render("⚑ LINTING"))
	}
	badgeStr := ""
	if len(badges) > 0 {
		badgeStr = "  " + strings.Join(badges, " ")
	}

	// ── Inventory name (right-aligned) ────────────────────────────────────
	invName := ""
	if a.config.InventoryPath != "" {
		invName = badgeMuted.Render("  " + filepath.Base(a.config.InventoryPath))
	}

	left := logo + badgeStr

	// ── Tab bar ───────────────────────────────────────────────────────────
	type tab struct {
		num   string
		label string
		panel core.Panel
	}
	tabs := []tab{
		{"1", "Inventory", core.PanelInventory},
		{"2", "Playbooks", core.PanelPlaybooks},
		{"3", "Status", core.PanelStatus},
		{"4", "Logs", core.PanelLogs},
	}

	var tabParts []string
	for _, t := range tabs {
		numPart := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#475569")).
			Render(t.num + " ")
		if t.panel == a.focused {
			// Active tab: bright, underlined appearance
			active := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#06B6D4")).Bold(true).
				Background(lipgloss.Color("#0F2233")).
				Padding(0, 1).
				Render(t.num + " " + t.label)
			tabParts = append(tabParts, active)
		} else {
			inactive := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#64748B")).
				Padding(0, 1).
				Render(numPart + t.label)
			tabParts = append(tabParts, inactive)
		}
	}
	tabBar := strings.Join(tabParts, sepStyle.Render(" "))

	right := tabBar + invName

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	return headerStyle.
		Width(a.width).
		Padding(0, 1).
		Render(left + strings.Repeat(" ", gap) + right)
}

func (a *App) renderStatusBar() string { return a.actionFooter() }
func (a *App) helpContent() string     { return a.actionsHelp() }

// ─── Helpers ─────────────────────────────────────────────────────────────────

// inventoryBaseDir returns the directory containing the inventory file,
// or the working directory if no inventory is configured.
func inventoryBaseDir(cfg Config) string {
	if cfg.InventoryPath != "" && !strings.Contains(cfg.InventoryPath, ",") {
		return filepath.Dir(cfg.InventoryPath)
	}
	return cfg.WorkDir
}

// editorOpenVarsFile finds or creates a vars file then opens it in $EDITOR.
func editorOpenVarsFile(baseDir, subdir, entityName string) tea.Cmd {
	return func() tea.Msg {
		path, err := editor.FindOrCreate(baseDir, subdir, entityName)
		if err != nil {
			return editor.DoneMsg{Err: err}
		}
		// We must run tea.ExecProcess synchronously via a Cmd.
		// Return an openEditorCmd so app can dispatch it as a tea.Cmd.
		return editorOpenPathMsg{path: path}
	}
}

// editorOpenPathMsg carries a path that should be opened in the editor.
type editorOpenPathMsg struct{ path string }

// handleMouseClick focuses the panel that contains the clicked cell.
func (a *App) handleMouseClick(x, y int) {
	if a.mode != AppModeNormal || a.logsFullscreen {
		return
	}
	// Row 0 = header, rows 1..topH = top panels, rows topH+1.. = logs.
	topH := topPanelHeight
	if topH > a.height-6 {
		topH = a.height - 6
	}

	if y == 0 || y >= a.height-1 {
		return // header or statusbar
	}

	if y >= 1 && y <= topH {
		// Top row: determine which column.
		invW := a.width / 4
		pbW := a.width / 4
		switch {
		case x < invW:
			a.focused = core.PanelInventory
		case x < invW+pbW:
			a.focused = core.PanelPlaybooks
		default:
			a.focused = core.PanelStatus
		}
	} else {
		a.focused = core.PanelLogs
	}
	a.updateFocus()
}

func (a *App) cycleFocus(dir int) {
	order := []core.Panel{
		core.PanelInventory,
		core.PanelPlaybooks,
		core.PanelStatus,
		core.PanelLogs,
	}
	idx := 0
	for i, p := range order {
		if p == a.focused {
			idx = i
			break
		}
	}
	a.focused = order[(idx+dir+len(order))%len(order)]
	a.updateFocus()
}

func (a *App) updateFocus() {
	a.invPanel.SetFocused(a.focused == core.PanelInventory)
	a.pbPanel.SetFocused(a.focused == core.PanelPlaybooks)
	a.statusPanel.SetFocused(a.focused == core.PanelStatus)
	a.logsPanel.SetFocused(a.focused == core.PanelLogs)
}

func (a *App) resizePanels() {
	if a.workbench != nil {
		a.syncWorkbenchLayout()
	}
	a.review.viewport.Width = max(1, a.width-2)
	a.review.viewport.Height = max(1, a.height-7)
	a.reflowReview()

	available := a.height - 3
	invW := a.width / 4
	pbW := a.width / 4
	statusW := a.width - invW - pbW

	if a.logsFullscreen {
		a.invPanel.SetSize(invW-4, 1)
		a.pbPanel.SetSize(pbW-4, 1)
		a.statusPanel.SetSize(statusW-4, 1)
		a.logsPanel.SetSize(a.width-4, available-2)
	} else {
		topH := topPanelHeight
		if topH > available-4 {
			topH = available - 4
		}
		botH := available - topH
		a.invPanel.SetSize(invW-4, topH-2)
		a.pbPanel.SetSize(pbW-4, topH-2)
		a.statusPanel.SetSize(statusW-4, topH-2)
		a.logsPanel.SetSize(a.width-4, botH-2)
	}

	a.adhocOverlay.width = a.width
	a.adhocOverlay.height = a.height
	a.extraVarsOverlay.width = a.width
	a.extraVarsOverlay.height = a.height
	a.tagsOverlay.width = a.width
	a.tagsOverlay.height = a.height
	a.vaultOverlay.width = a.width
	a.vaultOverlay.height = a.height
	a.historyOverlay.width = a.width
	a.historyOverlay.height = a.height
	a.rolesOverlay.width = a.width
	a.rolesOverlay.height = a.height
	a.envSwitchOverlay.width = a.width
	a.envSwitchOverlay.height = a.height
	a.sshProfileOverlay.width = a.width
	a.sshProfileOverlay.height = a.height
	a.galaxyOverlay.width = a.width
	a.galaxyOverlay.height = a.height
	a.runProfilesOverlay.width = a.width
	a.runProfilesOverlay.height = a.height
	a.pbViewerOverlay.width = a.width
	a.pbViewerOverlay.height = a.height
	if a.width < 100 || a.height < 22 {
		w, h := max(1, a.width), max(1, a.height-5)
		a.invPanel.SetSize(w, h)
		a.pbPanel.SetSize(w, h)
		a.statusPanel.SetSize(w, h)
		a.logsPanel.SetSize(w, h)
	}
}

// ─── Run lifecycle ────────────────────────────────────────────────────────────

func (a *App) handleRunFinished(msg runner.RunFinishedMsg) tea.Cmd {
	a.running = false
	a.statusPanel.SetRunning(false)
	if a.cancelRun != nil {
		a.cancelRun()
		a.cancelRun = nil
	}
	a.cleanupVaultFile()
	a.cleanupTempPlaybook()

	// Collect failed hosts for retry.
	a.retryHosts = a.statusPanel.FailedHosts()

	var finishCmds []tea.Cmd
	// Persist history through an effect, reporting a failed save.
	if a.runRecord != nil {
		a.runRecord.EndTime = time.Now()
		a.runRecord.ExitCode = msg.ExitCode
		a.runRecord.HostStats = a.statusPanel.HostStatsMap()
		record := *a.runRecord
		finishCmds = append(finishCmds, func() tea.Msg { return historySavedMsg{err: history.Save(&record)} })
		a.runRecord = nil
	}

	if msg.Err != nil {
		a.statusMsg = "Run error: " + msg.Err.Error()
	} else if msg.ExitCode == 0 {
		a.statusMsg = "Completed successfully ✓"
	} else {
		failStr := ""
		if len(a.retryHosts) > 0 {
			failStr = fmt.Sprintf("  [R] retry %d failed host(s)", len(a.retryHosts))
		}
		a.statusMsg = fmt.Sprintf("Exit code %d%s", msg.ExitCode, failStr)
	}

	// Desktop notification.
	if a.notifyOnFinish {
		pbName := a.pbPanel.SelectedPlaybook()
		name := "playbook"
		if pbName != nil {
			name = pbName.Name
		}
		dur := ""
		if msg.Duration > 0 {
			dur = msg.Duration.Round(time.Second).String()
		}
		exitCode := msg.ExitCode
		go notify.Send(notify.RunResult{
			PlaybookName: name,
			ExitCode:     exitCode,
			Duration:     dur,
		})
	}

	if a.quitting {
		return func() tea.Msg {
			for _, cmd := range finishCmds {
				if cmd != nil {
					cmd()
				}
			}
			return tea.Quit()
		}
	}
	return tea.Batch(finishCmds...)
}

// switchInventory reloads the inventory from a new path.
func (a *App) switchInventory(path string) tea.Cmd {
	a.config.InventoryPath = path
	a.statusMsg = "Switching to " + filepath.Base(path) + "…"
	return a.reloadProject()
}

func (a *App) reloadProject() tea.Cmd {
	a.config.generation++
	a.config.Runtime.WorkDir = a.config.WorkDir
	galaxy.SetContext(a.projectContext(), a.config.Runtime)
	a.workbench.generation++
	return tea.Batch(loadInventoryCmd(a.config), loadPlaybooksCmd(a.config))
}

func (a *App) cleanupVaultFile() {
	if a.vaultPasswordFile != "" {
		_ = os.Remove(a.vaultPasswordFile)
		a.vaultPasswordFile = ""
	}
}

func (a *App) cleanupTempPlaybook() {
	if a.tempPlaybook != "" {
		_ = os.Remove(a.tempPlaybook)
		a.tempPlaybook = ""
	}
}

func (a *App) scanVaultCmd() tea.Cmd {
	workDir := a.config.WorkDir
	return func() tea.Msg {
		files := vault.FindEncryptedFiles(workDir)
		return vaultScanDoneMsg{hasVault: len(files) > 0}
	}
}

func truncateStr(s string, max int) string {
	if max < 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// ─── Startup commands ─────────────────────────────────────────────────────────

func loadInventoryCmd(cfg Config) tea.Cmd {
	return func() tea.Msg {
		path := cfg.InventoryPath
		if path == "" {
			paths := inventory.Discover(cfg.WorkDir)
			if len(paths) == 0 {
				return inventoryLoadedMsg{
					inv: &core.Inventory{
						Hosts:  make(map[string]*core.Host),
						Groups: make(map[string]*core.Group),
					},
					path: "", generation: cfg.generation,
				}
			}
			path = paths[0]
		}
		inv, err := inventory.Parse(path)
		if err != nil {
			return errMsg{err: fmt.Errorf("parse inventory %s: %w", filepath.Base(path), err), generation: cfg.generation}
		}
		return inventoryLoadedMsg{inv: inv, path: path, generation: cfg.generation}
	}
}

// playbookStdNames are the well-known root-level playbook file names.
var playbookStdNames = []string{
	"playbook.yml", "playbook.yaml",
	"site.yml", "site.yaml",
}

func loadPlaybooksCmd(cfg Config) tea.Cmd {
	return func() tea.Msg {
		dir := cfg.PlaybookDir
		if dir == "" {
			dir = cfg.WorkDir
		}
		pbs, err := inventory.DiscoverPlaybooks(dir)
		if err != nil {
			return errMsg{err: fmt.Errorf("discover playbooks: %w", err), generation: cfg.generation}
		}

		// If nothing found, also search the parent directory for standard names.
		if len(pbs) == 0 {
			parent := filepath.Dir(dir)
			if parent != dir {
				pbsParent, _ := inventory.DiscoverPlaybooks(parent)
				pbs = append(pbs, pbsParent...)
			}
		}

		// Additionally surface any standard-named playbooks in . and .. that
		// the walker might have skipped (e.g. site.yml at repo root above dir).
		seen := map[string]bool{}
		for _, p := range pbs {
			seen[p.Path] = true
		}
		for _, searchDir := range []string{dir, filepath.Dir(dir)} {
			for _, name := range playbookStdNames {
				p := filepath.Join(searchDir, name)
				abs, _ := filepath.Abs(p)
				if seen[abs] {
					continue
				}
				if extra, ok := inventory.ParseSinglePlaybook(p); ok {
					seen[abs] = true
					pbs = append(pbs, extra)
				}
			}
		}

		return playbooksLoadedMsg{pbs: pbs, generation: cfg.generation}
	}
}

// ─── v0.6 helpers ─────────────────────────────────────────────────────────────

// checkGalaxyBinary returns an error if ansible-galaxy is not available.
func checkGalaxyBinary() error {
	return galaxy.CheckBinary()
}

// applyRunProfile replaces the draft and reloads the project as one operation.
func (a *App) applyRunProfile(p runprofiles.Profile) tea.Cmd {
	if a.running {
		a.statusMsg = "Wait for the active run before changing profiles"
		return nil
	}
	if p.WorkDir != "" {
		a.config.WorkDir = p.WorkDir
	}
	if filepath.IsAbs(p.Playbook) {
		a.config.PlaybookDir = filepath.Dir(p.Playbook)
	}
	a.config.InventoryPath = p.Inventory
	a.pbPanel.SetLimit(p.Limit)
	a.pbPanel.SetActiveTags(strings.Join(p.Tags, ","))
	a.extraVarsRaw = p.ExtraVars
	a.pbPanel.SetExtraVars(p.ExtraVars)
	a.pbPanel.SetCheckMode(p.CheckMode)
	a.pbPanel.SetDiffMode(p.DiffMode)
	a.profileNeedsSelection = true
	a.pendingProfile = &p
	a.statusMsg = "Loading profile: " + p.Name
	return a.reloadProject()
}
func (a *App) selectProfilePlaybook(p runprofiles.Profile) {
	if p.Playbook == "" {
		return
	}
	if a.pbPanel.SelectByPath(p.Playbook) || a.pbPanel.SelectByNameUnique(p.Playbook) {
		a.profileNeedsSelection = false
		a.statusMsg = "Profile loaded: " + p.Name
	} else {
		a.profileNeedsSelection = true
		a.statusMsg = "Profile target missing or ambiguous; navigate or Enter to explicitly select a playbook"
	}
}

// Keep the UI alive until the owned child reports it has stopped and reaped.
func (a *App) requestQuit() tea.Cmd {
	a.Close()
	if a.running || a.linting || a.runtimeBusy {
		a.quitting = true
		a.mode = AppModeNormal
		a.statusMsg = "Cancelling active operation before exit…"
		return nil
	}
	return tea.Quit
}
