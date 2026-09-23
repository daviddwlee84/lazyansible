package ui

import (
	"path/filepath"
	"reflect"

	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) openRolesWorkspace() tea.Cmd {
	a.openWorkspace(workspaceRoles)
	return a.refreshRolesWorkspace()
}

// refreshRolesWorkspace changes only data, preserving the user's current focus.
func (a *App) refreshRolesWorkspace() tea.Cmd {
	return a.rolesOverlay.Open(a.ctx, filepath.Join(a.config.WorkDir, "roles"), a.pbPanel.SelectedPlaybook(), a.config.InventoryPath, a.pbPanel.CurrentLimit())
}
func (a *App) acceptRolesResult(msg tea.Msg) bool { return a.rolesOverlay.Apply(msg) }
func (a *App) rolesWorkspaceTyping() bool         { return a.rolesOverlay.filtering }
func (a *App) rolesWorkspaceView() string {
	width, height := a.workspaceBodySize()
	return a.rolesOverlay.ContentView(width, height)
}
func (a *App) updateRolesWorkspace(msg tea.Msg) tea.Cmd {
	o := a.rolesOverlay
	o.width, o.height = a.workspaceBodySize()
	if o.filtering {
		return o.Update(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "r":
			return a.reviewCurrentPlaybook()
		case "t":
			return a.openTagsDraft(nil)
		case "s":
			return a.applyRoleTags()
		}
	}
	return o.Update(msg)
}
func (a *App) roleContextCurrent() bool {
	return a.rolesOverlay.context == roleContext(filepath.Join(a.config.WorkDir, "roles"), a.pbPanel.SelectedPlaybook())
}
func (a *App) roleTagsAvailable() bool {
	return a.roleContextCurrent() && !a.rolesOverlay.loading && a.rolesOverlay.err == nil && len(a.rolesOverlay.DeclaredTags()) > 0
}
func (a *App) standaloneRoleAvailable() bool {
	if !a.roleContextCurrent() || a.running || a.linting || a.runtimeBusy {
		return false
	}
	_, ok := a.rolesOverlay.StandaloneRequest()
	return ok
}
func (a *App) applyRoleTags() tea.Cmd {
	if !a.roleTagsAvailable() {
		a.statusMsg = "Select a literal role declaration with observed tags; role names are not tags"
		return nil
	}
	a.statusMsg = "Choose declared tags for the current playbook; other tasks may share them"
	return a.openTagsDraft(a.rolesOverlay.DeclaredTags())
}
func (a *App) reviewStandaloneRole() tea.Cmd {
	if !a.standaloneRoleAvailable() {
		a.statusMsg = "Select an available local role source before reviewing a standalone run"
		return nil
	}
	request, _ := a.rolesOverlay.StandaloneRequest()
	request.Inventory = a.config.InventoryPath
	request.Limit = a.pbPanel.CurrentLimit()
	request.Tags = ""
	// startRoleRun constructs the shared reviewed request. No parent-playbook tags
	// are carried across this explicit change of execution context.
	return a.startRoleRun(request)
}

func (a *App) rolesContextChanged() bool {
	o := a.rolesOverlay
	if o.dirty || !a.roleContextCurrent() {
		return true
	}
	pb := a.pbPanel.SelectedPlaybook()
	if pb == nil || o.inputPlaybook == nil {
		return pb != nil || o.inputPlaybook != nil
	}
	return !reflect.DeepEqual(pb.RoleDeclarations, o.inputPlaybook.RoleDeclarations)
}
