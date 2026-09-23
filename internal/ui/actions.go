package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazyansible/internal/core"
)

// action is the single source for dispatch, palette entries and contextual help.
type action struct {
	id, label string
	keys      []string
	kind      string
	enabled   bool
}

func (a *App) actions() []action {
	actions := []action{
		{"palette", "Actions", []string{":"}, "palette", true},
		{"config", "Inspect Ansible configuration", nil, "config", true},
		{"inventory", "Inspect resolved inventory", nil, "inventory", true},
		{"runtime", "Ansible runtime and updates", nil, "runtime", true},
		{"settings", "lazyansible settings", nil, "settings", true},
		{"clear-limit", "Clear target limit", nil, "clear-limit", a.pbPanel.CurrentLimit() != ""},
		{"help", "Help", []string{"?"}, "legacy", true},
		{"quit", "Quit", []string{"q"}, "legacy", true},
		{"focus-next", "Next panel", []string{"tab"}, "legacy", true},
		{"focus-prev", "Previous panel", []string{"shift+tab"}, "legacy", true},
		{"inventory-panel", "Inventory panel", []string{"1"}, "legacy", true},
		{"playbooks-panel", "Playbooks panel", []string{"2"}, "legacy", true},
		{"status-panel", "Status panel", []string{"3"}, "legacy", true},
		{"logs-panel", "Logs panel", []string{"4"}, "legacy", true},
		{"adhoc", "Ad-hoc module", []string{"!"}, "legacy", !a.running && !a.runtimeBusy},
		{"history", "Run history", []string{"H"}, "legacy", true},
		{"roles", "Role browser", []string{"O"}, "legacy", true},
		{"ssh", "SSH profiles", []string{"P"}, "legacy", true},
		{"profiles", "Run profiles", []string{"F"}, "legacy", true},
		{"galaxy", "Galaxy roles and collections", []string{"A"}, "legacy", !a.runtimeBusy},
		{"vault", "Vault password", []string{"V"}, "legacy", true},
		{"reload", "Refresh project", []string{"I"}, "legacy", true},
		{"retry", "Limit to failed hosts", []string{"R"}, "legacy", len(a.retryHosts) > 0 && !a.running},
	}
	if a.focused != core.PanelLogs {
		actions = append(actions, action{"environment", "Switch inventory", []string{"N"}, "legacy", true})
	} else {
		actions = append(actions, action{"environment", "Switch inventory", nil, "environment", true})
	}
	panel := func(id, label string, enabled bool, keys ...string) {
		actions = append(actions, action{id, label, keys, "panel", enabled})
	}
	panel("down", "Down", true, "j", "down")
	panel("up", "Up", true, "k", "up")
	panel("top", "Top (g / gg)", true, "g", "home")
	panel("bottom", "Bottom", true, "G", "end")
	panel("filter", "Search / filter", a.focused != core.PanelStatus, "/")
	if a.focused == core.PanelInventory {
		panel("inspect-host", "Inspect selection", a.invPanel.SelectedHost() != "" || a.invPanel.SelectedGroup() != "", "enter")
		panel("limit", "Set target limit", a.invPanel.SelectedHost() != "" || a.invPanel.SelectedGroup() != "", "s")
		panel("collapse", "Collapse / parent", true, "h", "left")
		panel("expand", "Expand / child", true, "l", "right")
		panel("toggle", "Toggle group", true, " ")
	}
	if a.focused == core.PanelPlaybooks {
		selected := a.pbPanel.SelectedPlaybook() != nil
		panel("view", "View playbook", selected, "enter", " ")
		panel("run", "Review and run", selected && !a.running && !a.linting && !a.runtimeBusy && a.pendingProfile == nil && !a.profileNeedsSelection, "r")
		panel("check", "Toggle check mode", true, "c")
		panel("diff", "Toggle diff mode", true, "d")
		for _, v := range []struct{ id, label, key string }{{"tags", "Select tags", "t"}, {"extra-vars", "Extra variables", "e"}, {"lint", "Lint playbook", "L"}} {
			actions = append(actions, action{v.id, v.label, []string{v.key}, "legacy", selected})
		}
	}
	if a.focused == core.PanelInventory || a.focused == core.PanelPlaybooks {
		actions = append(actions, action{"edit", "Edit selected source", []string{"E"}, "legacy", true})
	}
	if a.focused == core.PanelStatus {
		panel("inspect-status", "Inspect host", true, "enter")
	}
	if a.focused == core.PanelLogs {
		panel("next-match", "Next match", true, "n")
		panel("previous-match", "Previous match", true, "N")
		panel("log-filter", "Cycle log status filter", true, "f")
		panel("timestamps", "Toggle timestamps", true, "T")
		panel("half-down", "Half page down", true, "ctrl+d")
		panel("half-up", "Half page up", true, "ctrl+u")
		actions = append(actions, action{"fullscreen", "Toggle full-screen logs", []string{"Z"}, "legacy", true}, action{"export", "Export logs", []string{"X"}, "legacy", true}, action{"clear-logs", "Clear logs", []string{"ctrl+l"}, "legacy", true})
	}
	return actions
}
func (a *App) actionForKey(key string) (action, bool) {
	for _, v := range a.actions() {
		for _, k := range v.keys {
			if key == k {
				return v, true
			}
		}
	}
	return action{}, false
}
func (a *App) updateNormalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if (a.focused == core.PanelLogs && a.logsPanel.SearchActive()) || (a.focused == core.PanelInventory && a.invPanel.FilterActive()) || (a.focused == core.PanelPlaybooks && a.pbPanel.FilterActive()) {
		return a, a.delegateToPanel(msg)
	}
	if v, ok := a.actionForKey(msg.String()); ok {
		return a, a.dispatchAction(v)
	}
	if a.focused != core.PanelInventory {
		switch msg.String() {
		case "h", "left":
			a.cycleFocus(-1)
			return a, nil
		case "l", "right":
			a.cycleFocus(1)
			return a, nil
		}
	}
	return a, a.delegateToPanel(msg)
}
func (a *App) dispatchAction(v action) tea.Cmd {
	if !v.enabled {
		a.statusMsg = v.label + ": unavailable in the current state"
		return nil
	}
	switch v.kind {
	case "panel":
		if v.id == "view" && a.pendingProfile == nil {
			a.profileNeedsSelection = false
		}
		return a.delegateToPanel(keyFor(v.keys[0]))
	case "legacy":
		_, cmd := a.updateLegacyKeys(keyFor(v.keys[0]))
		return cmd
	case "environment":
		a.envSwitchOverlay.Scan(a.config.WorkDir, a.config.InventoryPath)
		a.mode = AppModeEnvSwitch
		return nil
	case "palette":
		a.palette.reset()
		a.mode = AppModePalette
		return nil
	case "clear-limit":
		a.pbPanel.SetLimit("")
		a.statusMsg = "Target limit cleared"
		return nil
	default:
		return a.openWorkbench(v.kind, "")
	}
}
func keyFor(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+l":
		return tea.KeyMsg{Type: tea.KeyCtrlL}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
func (a *App) actionFooter() string {
	if a.width < 1 {
		return ""
	}
	var parts []string
	switch a.focused {
	case core.PanelInventory:
		parts = []string{"enter inspect", "s limit", "h/l tree"}
	case core.PanelPlaybooks:
		parts = []string{"enter view", "r review", "c check", "d diff"}
	case core.PanelLogs:
		parts = []string{"/ search", "n/N matches", "G follow"}
	case core.PanelStatus:
		parts = []string{"enter inspect", "j/k select"}
	}
	// Resolve visible actions through the same registry; stale hints cannot invent actions.
	labels := []string{}
	for _, hint := range parts {
		key := strings.Fields(hint)[0]
		if strings.Contains(key, "/") {
			labels = append(labels, hint)
			continue
		}
		if v, ok := a.actionForKey(key); ok && v.enabled {
			labels = append(labels, key+" "+strings.ToLower(v.label))
		}
	}
	hint := strings.Join(labels, " · ") + " · : actions · ? help"
	if a.width < 90 {
		return ansi.Truncate(hint, a.width, "")
	}
	maxMsg := a.width - len(hint) - 3
	return ansi.Truncate(a.statusMsg, max(0, maxMsg), "…") + " | " + hint
}
func (a *App) actionsHelp() string {
	rows := []string{}
	for _, v := range a.actions() {
		if len(v.keys) == 0 {
			continue
		}
		suffix := ""
		if !v.enabled {
			suffix = " (unavailable)"
		}
		rows = append(rows, fmt.Sprintf("%-16s %s%s", strings.Join(v.keys, "/"), v.label, suffix))
	}
	rows = append(rows, ": opens searchable actions, Inspector and Runtime.", "Text fields own printable keys. Ctrl+C exits.")
	height := max(1, a.height-5)
	start := min(a.helpOffset, max(0, len(rows)-height))
	end := min(len(rows), start+height)
	return fitScreen("lazyansible — current actions\n\n"+strings.Join(rows[start:end], "\n")+"\n\nj/k scroll · g/G ends · Esc back", max(1, a.width-4), max(1, a.height-2))
}
