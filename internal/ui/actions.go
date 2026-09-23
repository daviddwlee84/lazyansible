package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	playbookReady := a.pbPanel.SelectedPlaybook() != nil && a.pendingProfile == nil && !a.profileNeedsSelection
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
		{"workspace-logs", "Workspace: Logs", []string{"4"}, "workspace-logs", true},
		{"adhoc", "Ad-hoc module", []string{"!"}, "legacy", !a.running && !a.runtimeBusy},
		{"history", "Run history", []string{"H"}, "legacy", true},
		{"workspace-roles", "Role browser", []string{"O"}, "workspace-roles", true},
		{"execution-preview", "Preview selected playbook scope", []string{"p"}, "execution-preview", playbookReady && !a.runtimeBusy},
		{"ssh", "SSH profiles", []string{"P"}, "legacy", true},
		{"profiles", "Run profiles", []string{"F"}, "legacy", true},
		{"galaxy", "Galaxy roles and collections", []string{"A"}, "legacy", !a.runtimeBusy},
		{"vault", "Vault password", []string{"V"}, "legacy", true},
		{"reload", "Refresh project", []string{"I"}, "legacy", true},
		{"retry", "Limit to failed hosts", []string{"R"}, "legacy", len(a.retryHosts) > 0 && !a.running},
	}
	if !a.workspaceLogsActive() {
		actions = append(actions, action{"environment", "Switch inventory", []string{"N"}, "environment", true})
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
	if !a.workspaceFocused() || a.workspace.tab != workspacePreview {
		panel("filter", "Search / filter", a.focused != core.PanelStatus, "/")
	}
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
	if a.workspaceFocused() {
		actions = append(actions,
			action{"workspace-previous", "Previous workspace tab", []string{"["}, "workspace-previous", true},
			action{"workspace-next", "Next workspace tab", []string{"]"}, "workspace-next", true},
			action{"workspace-zoom", "Zoom workspace", []string{"Z"}, "workspace-zoom", true},
			action{"run", "Review current playbook", []string{"r"}, "draft", playbookReady && !a.running && !a.linting && !a.runtimeBusy},
			action{"tags", "Select playbook tags", []string{"t"}, "draft", playbookReady},
			action{"check", "Toggle check mode", []string{"c"}, "draft", true},
			action{"diff", "Toggle diff mode", []string{"d"}, "draft", true},
			action{"extra-vars", "Extra variables", []string{"e"}, "legacy", playbookReady},
		)
		if a.workspace.tab != workspaceLogs {
			panel("detail", "Inspect selected content", true, "enter")
			panel("detail-left", "List focus", true, "h", "left")
			panel("detail-right", "Detail focus", true, "l", "right")
		}
		if a.workspace.tab == workspaceRoles {
			panel("role-scope", "Related / all project roles", true, "a")
			panel("role-source", "Browse role source files", true, "f")
			actions = append(actions,
				action{"role-tags", "Use this role's declared playbook tags", []string{"s"}, "role-tags", a.roleTagsAvailable()},
				action{"standalone-role", "Review standalone role (separate playbook context)", nil, "standalone-role", a.standaloneRoleAvailable() && !a.running && !a.linting && !a.runtimeBusy},
			)
		}
	}
	if a.workspaceLogsActive() {
		panel("next-match", "Next match", true, "n")
		panel("previous-match", "Previous match", true, "N")
		panel("log-filter", "Cycle log status filter", true, "f")
		panel("timestamps", "Toggle timestamps", true, "T")
		panel("half-down", "Half page down", true, "ctrl+d")
		panel("half-up", "Half page up", true, "ctrl+u")
		actions = append(actions, action{"export", "Export logs", []string{"X"}, "legacy", true}, action{"clear-logs", "Clear logs", []string{"ctrl+l"}, "legacy", true})
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
	if a.workspaceInputActive() || (a.focused == core.PanelInventory && a.invPanel.FilterActive()) || (a.focused == core.PanelPlaybooks && a.pbPanel.FilterActive()) {
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
	case "workspace-logs":
		a.openWorkspace(workspaceLogs)
		return nil
	case "workspace-roles":
		return a.openRolesWorkspace()
	case "execution-preview":
		return a.openExecutionPreview()
	case "workspace-previous":
		return a.cycleWorkspace(-1)
	case "workspace-next":
		return a.cycleWorkspace(1)
	case "workspace-zoom":
		a.workspace.zoom = !a.workspace.zoom
		a.resizePanels()
		return nil
	case "role-tags":
		return a.applyRoleTags()
	case "standalone-role":
		return a.reviewStandaloneRole()
	case "draft":
		switch v.id {
		case "run":
			return a.reviewCurrentPlaybook()
		case "tags":
			return a.openTagsDraft(nil)
		case "check":
			a.toggleDraftCheck()
		case "diff":
			a.toggleDraftDiff()
		}
		return nil
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
	if a.mode == AppModeRunReview {
		return ansi.Truncate("Tab choose · Enter confirm · j/k scroll · Esc cancel", a.width, "")
	}
	if a.mode == AppModeTagsBrowser {
		return ansi.Truncate("Tags · / filter · Space toggle · Enter apply · Esc back", a.width, "")
	}
	var parts []string
	switch a.focused {
	case core.PanelInventory:
		parts = []string{"enter inspect", "s limit", "h/l tree"}
	case core.PanelPlaybooks:
		parts = []string{"enter view", "r review", "c check", "d diff"}
	case core.PanelLogs:
		switch a.workspace.tab {
		case workspaceRoles:
			parts = []string{"enter inspect", "h/l detail", "t tags", "r review"}
		case workspacePreview:
			parts = []string{"h/l detail", "p refresh", "t tags", "r review"}
		default:
			parts = []string{"/ search", "n/N matches", "G follow"}
		}
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
	if a.workspaceFocused() {
		hint = "[ ] tabs · " + hint
	}
	if a.width < 90 {
		return ansi.Truncate(hint, a.width, "")
	}
	maxMsg := a.width - len(hint) - 3
	return ansi.Truncate(a.statusMsg, max(0, maxMsg), "…") + " | " + hint
}

// helpGroup changes presentation order only; dispatch and availability continue
// to come from the same action registry used by the dashboard and palette.
func helpGroup(v action) int {
	switch v.id {
	case "down", "up", "top", "bottom", "focus-next", "focus-prev",
		"inventory-panel", "playbooks-panel", "status-panel", "workspace-logs", "workspace-previous", "workspace-next":
		return 1
	case "run", "check", "diff", "tags", "extra-vars", "lint", "edit", "workspace-zoom", "export", "clear-logs":
		return 0
	}
	if v.kind == "panel" {
		return 0
	}
	return 2
}

func helpKeys(keys []string) string {
	names := make([]string, len(keys))
	for i, key := range keys {
		switch key {
		case " ":
			names[i] = "Space"
		case "enter":
			names[i] = "Enter"
		case "tab":
			names[i] = "Tab"
		case "shift+tab":
			names[i] = "Shift+Tab"
		case "home":
			names[i] = "Home"
		case "end":
			names[i] = "End"
		case "up":
			names[i] = "↑"
		case "down":
			names[i] = "↓"
		case "left":
			names[i] = "←"
		case "right":
			names[i] = "→"
		default:
			names[i] = strings.Replace(key, "ctrl+", "Ctrl+", 1)
		}
	}
	return strings.Join(names, " / ")
}

func (a *App) helpRows(width int) []string {
	var groups [3][]action
	for _, v := range a.actions() {
		if len(v.keys) != 0 {
			groups[helpGroup(v)] = append(groups[helpGroup(v)], v)
		}
	}
	panelNames := []string{"Inventory", "Playbooks", "Status", "Logs"}
	if a.workspaceFocused() {
		panelNames[3] = a.workspaceTitle()
	}
	titles := []string{panelNames[int(a.focused)] + " · current panel", "Navigation", "Global actions"}
	sectionStyle := lipgloss.NewStyle().Foreground(colorBorderFocus).Bold(true)
	var rows []string
	for i, group := range groups {
		if len(group) == 0 {
			continue
		}
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, sectionStyle.Render(ansi.Truncate(titles[i], width, "…")))
		for _, v := range group {
			label := v.label
			keyStyle, descriptionStyle := overlayActiveInputStyle, overlayItemStyle
			if !v.enabled {
				label += " (unavailable)"
				keyStyle, descriptionStyle = overlayLabelStyle, overlayMutedStyle
			}
			keys := helpKeys(v.keys)
			if width < 44 {
				for _, line := range strings.Split(ansi.Wrap(keys, width, ""), "\n") {
					rows = append(rows, keyStyle.Render(line))
				}
				indent := strings.Repeat(" ", min(2, max(0, width-1)))
				for _, line := range strings.Split(ansi.Wrap(label, max(1, width-len(indent)), ""), "\n") {
					rows = append(rows, indent+descriptionStyle.Render(line))
				}
				continue
			}
			const keyWidth = 16
			for j, line := range strings.Split(ansi.Wrap(label, width-keyWidth-2, ""), "\n") {
				key := ""
				if j == 0 {
					key = keys
				}
				keyColumn := keyStyle.Render(key) + strings.Repeat(" ", max(0, keyWidth-lipgloss.Width(key)))
				rows = append(rows, keyColumn+"  "+descriptionStyle.Render(line))
			}
		}
	}
	rows = append(rows, "")
	for _, line := range strings.Split(ansi.Wrap("Text fields own printable keys. Ctrl+C exits.", width, ""), "\n") {
		rows = append(rows, overlayLabelStyle.Render(line))
	}
	return rows
}

func (a *App) helpSize() (width, height, page int) {
	width = max(1, min(80, a.width-4))
	if a.width < 12 {
		width = max(1, a.width)
	}
	height = max(1, min(36, a.height-2))
	if a.height < 6 {
		height = max(1, a.height)
	}
	return width, height, max(1, height-4)
}

func (a *App) updateHelp(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	width, _, page := a.helpSize()
	last := max(0, len(a.helpRows(width))-page)
	a.helpOffset = min(max(0, a.helpOffset), last)
	switch key.String() {
	case "j", "down":
		a.helpOffset = min(last, a.helpOffset+1)
	case "k", "up":
		a.helpOffset = max(0, a.helpOffset-1)
	case "g", "home":
		a.helpOffset = 0
	case "G", "end":
		a.helpOffset = last
	case "q":
		a.mode = AppModeNormal
	}
	return nil
}

func (a *App) actionsHelp() string {
	if a.width <= 0 || a.height <= 0 {
		return ""
	}
	width, height, page := a.helpSize()
	rows := a.helpRows(width)
	start := min(max(0, a.helpOffset), max(0, len(rows)-page))
	end := min(len(rows), start+page)
	lines := []string{overlayTitleStyle.Render("Keyboard shortcuts"), ""}
	lines = append(lines, rows[start:end]...)
	for len(lines) < page+2 {
		lines = append(lines, "")
	}
	footer := fmt.Sprintf("%d–%d/%d · j/k ↑/↓ scroll · g/G ends · Esc back", start+1, end, len(rows))
	if width < 55 {
		footer = fmt.Sprintf("%d–%d/%d · ↑↓ scroll · Esc back", start+1, end, len(rows))
	}
	lines = append(lines, "", overlayLabelStyle.Render(ansi.Truncate(footer, width, "…")))
	// Place centers every line independently unless each already spans the same
	// width. Pad the complete left-aligned block before the shared modal layout.
	content := fitScreen(strings.Join(lines, "\n"), width, height)
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Left).Render(content)
}
