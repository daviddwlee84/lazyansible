package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/runprofiles"
)

func TestWorkspaceGridAndMouseShareLayout(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	l := a.layout()
	if l.compact || !l.workspaceVisible || l.workspace.y != l.top[0].y+l.top[0].height {
		t.Fatalf("inconsistent grid: %+v", l)
	}
	if _, height := a.workspaceBodySize(); height < 10 {
		t.Fatalf("workspace body too small at 140x40: %d", height)
	}
	for panel, rect := range l.top {
		a.handleMouseClick(rect.x+rect.width/2, rect.y+1)
		if a.focused != core.Panel(panel) {
			t.Fatalf("click in pane %d focused %d", panel, a.focused)
		}
	}
	a.workspace.tab = workspacePreview
	a.handleMouseClick(l.workspace.x+1, l.workspace.y+1)
	if a.focused != core.PanelLogs || a.workspace.tab != workspacePreview {
		t.Fatal("workspace focus changed active tab")
	}
	a.handleMouseClick(l.feedback.x+1, l.feedback.y)
	if a.focused != core.PanelLogs {
		t.Fatal("feedback row should not change panel focus")
	}
	view := ansi.Strip(a.View())
	for _, label := range []string{"Inventory (static)", "Playbooks", "Status", "4 Logs", "O Roles", "p Preview"} {
		if !strings.Contains(view, label) {
			t.Fatalf("workspace grid omitted %q", label)
		}
	}
}

func TestWorkspaceMouseTabsUseVisibleBoundsAndRespectModalInput(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	click := func(rect workspaceRect) tea.Cmd {
		_, cmd := a.Update(tea.MouseMsg{X: rect.x + 1, Y: rect.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		return cmd
	}
	if cmd := click(a.layout().tabs[workspacePreview]); cmd != nil || a.workspace.tab != workspacePreview {
		t.Fatal("Preview tab click should only switch presentation")
	}
	if cmd := click(a.layout().tabs[workspaceRoles]); cmd == nil || a.workspace.tab != workspaceRoles {
		t.Fatal("Roles tab click should schedule local discovery")
	}
	a.mode = AppModeRunReview
	click(a.layout().tabs[workspaceLogs])
	if a.workspace.tab != workspaceRoles {
		t.Fatal("tab click escaped active review")
	}
	a.mode = AppModeNormal
	a.Update(tea.WindowSizeMsg{Width: 8, Height: 12})
	if a.layout().tabs[workspacePreview].width != 0 {
		t.Fatal("offscreen tab retained a clickable rectangle")
	}
}

func TestWorkspaceMouseSwitchDoesNotLetHiddenLogSearchCaptureKeys(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.openWorkspace(workspaceLogs)
	a.Update(overlayKey("/"))
	a.Update(overlayKey("query"))
	roles := a.layout().tabs[workspaceRoles]
	a.Update(tea.MouseMsg{X: roles.x + 1, Y: roles.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	a.Update(overlayKey("tab"))
	if a.focused != core.PanelInventory {
		t.Fatal("hidden Logs search captured major-pane Tab navigation")
	}
	if a.logsPanel.SearchQuery() != "query" {
		t.Fatal("workspace tab switch should preserve the log search query")
	}
}

func TestWorkspaceEntriesTabsAndMajorFocusCycle(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(overlayKey("p")) // The resulting observation command is not executed.
	if a.workspace.tab != workspacePreview || a.focused != core.PanelLogs {
		t.Fatal("p must focus Preview")
	}
	a.Update(overlayKey("tab"))
	if a.focused != core.PanelInventory {
		t.Fatal("Tab was trapped inside workspace")
	}
	a.Update(keyFor("shift+tab"))
	if a.focused != core.PanelLogs || a.workspace.tab != workspacePreview {
		t.Fatal("returning to workspace lost active tab")
	}
	a.Update(overlayKey("4"))
	if a.workspace.tab != workspaceLogs || a.focused != core.PanelLogs {
		t.Fatal("4 must focus Logs directly")
	}
	a.Update(overlayKey("O"))
	if a.workspace.tab != workspaceRoles || a.mode != AppModeNormal {
		t.Fatal("O should open Roles in the main workspace")
	}
	_, cmd := a.Update(overlayKey("]"))
	if a.workspace.tab != workspacePreview || cmd != nil {
		t.Fatal("cycling into Preview must not trigger Ansible observation")
	}
	a.Update(overlayKey("]"))
	if a.workspace.tab != workspaceLogs {
		t.Fatal("workspace tabs should wrap")
	}
	a.Update(overlayKey("["))
	if a.workspace.tab != workspacePreview {
		t.Fatal("previous workspace tab failed")
	}
}

func TestWorkspaceScopesLogBindingsAndTyping(t *testing.T) {
	a := workbenchFixture(t)
	a.openWorkspace(workspaceLogs)
	if action, ok := a.actionForKey("N"); !ok || action.id != "previous-match" {
		t.Fatal("Logs N should search the previous match")
	}
	a.Update(overlayKey("O"))
	if action, ok := a.actionForKey("N"); !ok || action.id != "environment" {
		t.Fatal("Roles inherited a Logs-only shortcut")
	}
	if _, ok := a.actionForKey("T"); ok {
		t.Fatal("Roles should not advertise Logs timestamps")
	}
	a.Update(overlayKey("/"))
	query := "q[]pOtcr /"
	for _, key := range query {
		a.Update(overlayKey(string(key)))
	}
	if a.rolesOverlay.filter.Value() != query || a.workspace.tab != workspaceRoles || a.mode != AppModeNormal || a.ctx.Err() != nil {
		t.Fatal("workspace shortcuts intercepted role filter input")
	}
	a.Update(overlayKey("enter"))
	a.Update(overlayKey("]"))
	a.Update(overlayKey("["))
	if a.rolesOverlay.filter.Value() != query {
		t.Fatal("tab switching cleared the role filter")
	}
}

func TestWorkspaceZoomRestoresVisibleMajorFocus(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.openWorkspace(workspacePreview)
	a.Update(overlayKey("Z"))
	if !a.workspace.zoom || a.layout().top[0].height != 0 {
		t.Fatal("Z did not expand the workspace")
	}
	a.Update(overlayKey("1"))
	if a.workspace.zoom || a.focused != core.PanelInventory || a.layout().top[0].height == 0 {
		t.Fatal("focus moved to an invisible pane while zoomed")
	}
	a.Update(keyFor("shift+tab"))
	if a.workspace.tab != workspacePreview {
		t.Fatal("zoom/focus transition lost active tab")
	}
}

func TestWorkspaceReviewStaysBelowVisibleUpperPanes(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.focused = core.PanelPlaybooks
	a.updateFocus()
	request := a.selectedRequest()
	_ = a.prepareRun(request)
	a.Update(runPreparedMsg{id: a.reviewID, plan: ansible.RunPlan{Request: request, Preview: "ansible-playbook site.yml"}})
	view := ansi.Strip(a.View())
	for _, label := range []string{"Inventory (static)", "Playbooks", "Status", "Review execution", "Cancel"} {
		if !strings.Contains(view, label) {
			t.Fatalf("review hid %q", label)
		}
	}
	row := -1
	for i, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Review execution") {
			row = i
			break
		}
	}
	if row < a.layout().workspace.y {
		t.Fatal("review escaped workspace rectangle")
	}
	original := a.focused
	a.handleMouseClick(a.layout().top[0].x+1, a.layout().top[0].y+1)
	a.Update(overlayKey("1"))
	if a.focused != original {
		t.Fatal("review allowed target pane interaction")
	}
	a.Update(overlayKey("tab"))
	if !a.review.confirm || a.focused != original {
		t.Fatal("review Tab should select Run, not change panes")
	}
	a.Update(overlayKey("esc"))
	if a.mode != AppModeNormal || a.focused != core.PanelPlaybooks {
		t.Fatal("review cancellation lost originating focus")
	}
}

func TestWorkspaceTagsAreContainedAndReturnToOrigin(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.focused = core.PanelPlaybooks
	a.updateFocus()
	_ = a.openTagsDraft(nil)
	view := ansi.Strip(a.View())
	if !strings.Contains(view, "Inventory (static)") || !strings.Contains(view, "site.yml › Tags") {
		t.Fatal("compact Tags did not keep workspace context")
	}
	a.Update(overlayKey("esc"))
	if a.mode != AppModeNormal || a.focused != core.PanelPlaybooks {
		t.Fatal("Tags cancellation lost originating focus")
	}
}

func TestWorkspaceKeepsRunContextSeparateFromCurrentDraft(t *testing.T) {
	a := workbenchFixture(t)
	a.lastRunRequest = &ansible.RunRequest{Kind: "playbook", Playbook: "/old/previous-run.yml", Project: ansible.ProjectContext{WorkDir: "/old", Inventory: "/old/inventory.ini"}}
	a.focused = core.PanelPlaybooks
	a.updateFocus()
	if strings.Contains(ansi.Strip(a.View()), "previous-run.yml") {
		t.Fatal("compact Playbooks displays old run as its current draft")
	}
	a.Update(overlayKey("4"))
	if !strings.Contains(ansi.Strip(a.View()), "previous-run.yml") {
		t.Fatal("Logs lost its frozen run context")
	}
}

func TestWorkspaceRapidResizeBoundsAcrossTabsAndNestedModes(t *testing.T) {
	a := workbenchFixture(t)
	for _, size := range [][2]int{{140, 40}, {100, 22}, {80, 24}, {48, 12}, {8, 3}, {1, 1}, {0, 0}, {140, 40}} {
		a.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, tab := range []workspaceTab{workspaceLogs, workspaceRoles, workspacePreview} {
			a.openWorkspace(tab)
			for _, mode := range []AppMode{AppModeNormal, AppModeRunReview, AppModeTagsBrowser} {
				a.mode = mode
				a.resizePanels()
				view := a.View()
				if !utf8.ValidString(view) {
					t.Fatal("invalid UTF-8 after workspace resize")
				}
				if size[0] == 0 || size[1] == 0 {
					continue
				}
				if lipgloss.Height(view) > size[1] {
					t.Fatalf("height overflow %v tab %d mode %d", size, tab, mode)
				}
				for _, line := range strings.Split(view, "\n") {
					if lipgloss.Width(line) > size[0] {
						t.Fatalf("width overflow %v tab %d mode %d: %q", size, tab, mode, line)
					}
				}
			}
		}
	}
}

func TestWorkspaceProfileDoesNotOverwriteOtherPlaybookTags(t *testing.T) {
	a := workbenchFixture(t)
	first := a.pbPanel.SelectedPlaybook().Path
	a.pbPanel.SetActiveTags("first-only")
	a.syncExecutionDraft()
	second := filepath.Join(a.config.WorkDir, "second.yml")
	if err := os.WriteFile(second, []byte("- hosts: all\n  tasks: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cmd := a.Update(RunProfileLoadMsg{Profile: runprofiles.Profile{Name: "second", WorkDir: a.config.WorkDir, Playbook: second, Tags: []string{"second-only"}}})
	runProjectCommand(t, a, cmd)
	if a.pbPanel.SelectedPlaybook().Path != second || a.pbPanel.SelectedTags()[0] != "second-only" {
		t.Fatal("profile did not resolve the second target")
	}
	if !a.pbPanel.SelectByPath(first) {
		t.Fatal("first playbook disappeared")
	}
	a.syncExecutionDraft()
	if got := strings.Join(a.pbPanel.SelectedTags(), ","); got != "first-only" {
		t.Fatalf("profile for second playbook overwrote first playbook's tags: %q", got)
	}
}

func TestWorkspacePreviewKeepsSelectedSectionVisibleAtShortHeight(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	a.openWorkspace(workspacePreview)
	a.Update(overlayKey("G"))
	if a.executionPreview.section != 3 {
		t.Fatal("End did not select final Preview section")
	}
	if !strings.Contains(ansi.Strip(a.View()), "Ansible output") {
		t.Fatal("short Preview hides the currently selected section")
	}
}
