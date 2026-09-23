package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/runner"
	"github.com/daviddwlee84/lazyansible/internal/runprofiles"
	"github.com/daviddwlee84/lazyansible/internal/ui/panels"
)

func workbenchFixture(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	a := New(Config{WorkDir: root, ConfigPath: filepath.Join(root, "config.yml")})
	t.Cleanup(a.Close)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	inv := &core.Inventory{
		OrderedGroups: []string{"web"},
		Hosts:         map[string]*core.Host{"web-a": {Name: "web-a", Groups: []string{"web"}}},
		Groups:        map[string]*core.Group{"web": {Name: "web", Hosts: []string{"web-a"}}},
	}
	a.Update(inventoryLoadedMsg{inv: inv, generation: a.config.generation})
	playbook := filepath.Join(root, "site.yml")
	if err := os.WriteFile(playbook, []byte("- hosts: all\n  tasks: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.Update(playbooksLoadedMsg{pbs: []*core.Playbook{{Name: "site", Path: playbook, Hosts: []string{"all"}}}, generation: a.config.generation})
	return a
}

func appModes() []AppMode {
	return []AppMode{AppModeNormal, AppModeAdHoc, AppModeExtraVars, AppModeTagsBrowser, AppModeVault, AppModeHistory, AppModeRoles, AppModeEnvSwitch, AppModeSSHProfile, AppModeHelp, AppModeGalaxy, AppModeRunProfiles, AppModePlaybookViewer, AppModeWorkbench, AppModeRunReview, AppModePalette}
}

func TestBackgroundRunEventsReachEveryOverlay(t *testing.T) {
	for _, mode := range appModes() {
		t.Run(string(rune('A'+mode)), func(t *testing.T) {
			a := workbenchFixture(t)
			a.mode = mode
			a.running = true
			cancelled := false
			a.cancelRun = func() { cancelled = true }
			a.Update(runner.LogMsg{Line: core.LogLine{Text: "finished task", Level: core.LogLevelFailed}})
			a.Update(runner.HostStatusMsg{Host: "web-a", Status: core.TaskStatusFailed, Task: "one"})
			a.Update(runner.RunFinishedMsg{ExitCode: 2, Duration: time.Second})
			if a.running || !cancelled {
				t.Fatalf("mode %d lost run completion", mode)
			}
			if a.mode != mode {
				t.Fatalf("completion replaced mode %d with %d", mode, a.mode)
			}
			lines := a.logsPanel.Lines()
			if len(lines) != 2 || lines[0].Text != "finished task" || !strings.Contains(lines[1].Text, "Exit code 2") {
				t.Fatalf("mode %d lost logs: %+v", mode, lines)
			}
			if len(a.retryHosts) != 1 || a.retryHosts[0] != "web-a" {
				t.Fatalf("mode %d lost host result: %v", mode, a.retryHosts)
			}
			a.linting = true
			a.Update(lintFinishedMsg{exitCode: 0})
			if a.linting {
				t.Fatalf("mode %d lost lint completion", mode)
			}
		})
	}
}

func TestStaleDiscoveryAndInspectionCannotReplaceCurrentContext(t *testing.T) {
	a := workbenchFixture(t)
	current := a.inventory
	a.config.generation++
	a.Update(inventoryLoadedMsg{inv: &core.Inventory{}, path: "/old/inventory", generation: a.config.generation - 1})
	a.Update(playbooksLoadedMsg{generation: a.config.generation - 1})
	if a.inventory != current || a.config.InventoryPath == "/old/inventory" || a.pbPanel.SelectedPlaybook() == nil {
		t.Fatal("stale project discovery replaced current state")
	}
	a.statusMsg = "current"
	a.Update(errMsg{err: errors.New("old failure"), generation: a.config.generation - 1})
	if a.statusMsg != "current" {
		t.Fatal("stale project failure replaced status")
	}
	w := a.workbench
	w.topic = "inventory"
	w.generation = 4
	w.pending = true
	w.setRows([]browserRow{{id: "current", label: "Current", detail: "retained"}})
	a.mode = AppModeHelp
	for _, stale := range []inspectionMsg{
		{generation: 3, topic: "inventory", rows: []browserRow{{id: "old", label: "Old"}}},
		{generation: 4, topic: "config", rows: []browserRow{{id: "wrong-topic", label: "Wrong"}}},
	} {
		a.Update(stale)
	}
	if !w.pending || w.selected().id != "current" {
		t.Fatal("stale inspection replaced pending current observation")
	}
	a.Update(inspectionMsg{generation: 4, topic: "inventory", rows: []browserRow{{id: "new", label: "New"}}, source: "fresh"})
	if w.pending || w.selected().id != "new" || a.mode != AppModeHelp {
		t.Fatal("current observation should update rows without replacing overlay")
	}
	a.Update(inspectionMsg{generation: 4, topic: "inventory", err: errors.New("offline")})
	if w.selected().id != "new" || w.err != "offline" {
		t.Fatal("failed refresh should retain useful rows with error")
	}
}

func TestRootFiltersAndPaletteOwnPrintableKeys(t *testing.T) {
	for _, panel := range []core.Panel{core.PanelInventory, core.PanelPlaybooks, core.PanelLogs} {
		t.Run(string(rune('A'+panel)), func(t *testing.T) {
			a := workbenchFixture(t)
			a.focused = panel
			a.updateFocus()
			a.Update(overlayKey("/"))
			query := "jqkhNl/?: !"
			for _, key := range query {
				a.Update(overlayKey(string(key)))
			}
			if a.mode != AppModeNormal || a.focused != panel || a.ctx.Err() != nil {
				t.Fatalf("typing in panel %d triggered a global action", panel)
			}
			switch panel {
			case core.PanelInventory:
				if !a.invPanel.FilterActive() || a.invPanel.SelectedHost() != "" || a.invPanel.SelectedGroup() != "" {
					t.Fatal("inventory filter retained hidden selection")
				}
			case core.PanelPlaybooks:
				if !a.pbPanel.FilterActive() || a.pbPanel.SelectedPlaybook() != nil {
					t.Fatal("playbook filter retained hidden selection")
				}
			case core.PanelLogs:
				if a.logsPanel.SearchQuery() != query {
					t.Fatalf("search query = %q", a.logsPanel.SearchQuery())
				}
			}
			a.Update(overlayKey("enter"))
			if a.mode != AppModeNormal {
				t.Fatal("filter Enter should not inspect or run")
			}
		})
	}
	a := workbenchFixture(t)
	a.Update(overlayKey(":"))
	query := "jqkhNl/?: !"
	for _, key := range query {
		a.Update(overlayKey(string(key)))
	}
	if a.mode != AppModePalette || a.palette.input.Value() != query || a.ctx.Err() != nil {
		t.Fatalf("palette query %q changed app state", a.palette.input.Value())
	}
	a.Update(overlayKey("enter"))
	if a.mode != AppModePalette {
		t.Fatal("empty filtered palette must not dispatch a hidden action")
	}
}

func TestWorkbenchFilterAndClosedInspectionOwnership(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openWorkbench("inventory", "") // Do not execute its Ansible command.
	generation := a.workbench.generation
	a.Update(inspectionMsg{generation: generation, topic: "inventory", rows: []browserRow{{id: "one", label: "jqkhNl/?: !", target: "one"}}})
	a.Update(overlayKey("/"))
	query := "jqkhNl/?: !"
	for _, key := range query {
		a.Update(overlayKey(string(key)))
	}
	if a.mode != AppModeWorkbench || a.workbench.state().query != query || a.ctx.Err() != nil {
		t.Fatal("inspector filtering fired shortcuts")
	}
	a.Update(overlayKey("enter"))
	a.Update(overlayKey("esc"))
	if a.mode != AppModeNormal {
		t.Fatal("closing inspector did not restore dashboard")
	}
	a.Update(inspectionMsg{generation: generation, topic: "inventory", rows: []browserRow{{id: "late", label: "Late"}}})
	if a.mode != AppModeNormal || a.workbench.state().rows[0].id != "one" {
		t.Fatal("closed inspection accepted a late result")
	}
}

func TestRunReviewDefaultsToCancelAndRejectsLatePreparation(t *testing.T) {
	a := workbenchFixture(t)
	a.mode = AppModeHistory
	request := ansible.RunRequest{Kind: "playbook", Project: ansible.ProjectContext{WorkDir: a.config.WorkDir}, Playbook: a.pbPanel.SelectedPlaybook().Path}
	_ = a.prepareRun(request) // Preparation/execution is deliberately not invoked.
	firstID := a.reviewID
	a.Update(overlayKey("esc"))
	a.Update(runPreparedMsg{id: firstID, plan: ansible.RunPlan{Preview: "must be ignored"}})
	if a.mode != AppModeHistory || a.review.plan != nil || a.running {
		t.Fatal("cancelled preparation was accepted")
	}
	_ = a.prepareRun(request)
	a.Update(runPreparedMsg{id: a.reviewID, plan: ansible.RunPlan{Request: request, Preview: "ansible-playbook site.yml"}})
	if a.review.confirm {
		t.Fatal("review initially selects Run")
	}
	_, cmd := a.Update(overlayKey("enter"))
	if cmd != nil || a.running || a.mode != AppModeHistory {
		t.Fatal("initial Enter should cancel without an execution command")
	}
}

func TestAppRenderingStaysInsideRapidlyResizedTerminal(t *testing.T) {
	a := workbenchFixture(t)
	a.statusMsg = strings.Repeat("專案é👩🏽‍💻", 30)
	a.logsPanel.AddLine(core.LogLine{Text: strings.Repeat("專案é👩🏽‍💻", 30), Level: core.LogLevelInfo})
	a.workbench.topic = "inventory"
	a.workbench.setRows([]browserRow{{id: "host:one", label: strings.Repeat("專案", 30), detail: strings.Repeat("一行 é 👩🏽‍💻\n", 30)}})
	for _, size := range [][2]int{{120, 35}, {80, 24}, {40, 12}, {8, 2}, {1, 1}, {80, 24}, {110, 24}, {0, 0}, {80, 24}} {
		a.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, mode := range appModes() {
			a.mode = mode
			for _, panel := range []core.Panel{core.PanelInventory, core.PanelPlaybooks, core.PanelStatus, core.PanelLogs} {
				a.focused = panel
				a.updateFocus()
				view := a.View()
				if !utf8.ValidString(view) {
					t.Fatalf("mode %d returned invalid UTF-8", mode)
				}
				if size[0] == 0 || size[1] == 0 {
					continue
				} // Before the first usable size the initialization label is allowed.
				if got := lipgloss.Height(view); got > size[1] {
					t.Fatalf("mode %d panel %d height %d exceeds %v", mode, panel, got, size)
				}
				for _, line := range strings.Split(view, "\n") {
					if got := lipgloss.Width(line); got > size[0] {
						t.Fatalf("mode %d panel %d line width %d exceeds %v: %q", mode, panel, got, size, line)
					}
				}
			}
		}
	}
}

// runProjectCommand drains only local discovery effects, never a run or runtime command.
func runProjectCommand(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			runProjectCommand(t, a, child)
		}
		return
	}
	switch msg.(type) {
	case inventoryLoadedMsg, playbooksLoadedMsg, errMsg:
		_, next := a.Update(msg)
		runProjectCommand(t, a, next)
	default:
		t.Fatalf("unexpected effect in local discovery: %T", msg)
	}
}

func TestProfileApplyReloadsInventoryAndRestoresExactPlaybook(t *testing.T) {
	a := workbenchFixture(t)
	project := t.TempDir()
	playbook := filepath.Join(project, "deploy.yml")
	inv := filepath.Join(project, "inventory.ini")
	for path, contents := range map[string]string{playbook: "- hosts: all\n  tasks: []\n", inv: "[staging]\nnew-host\n"} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.pbPanel.SetLimit("old-production")
	a.pbPanel.SetActiveTags("old-tags")
	previousGeneration := a.config.generation
	_, cmd := a.Update(RunProfileLoadMsg{Profile: runprofiles.Profile{Name: "staging", WorkDir: project, Inventory: inv, Playbook: playbook, Limit: "", CheckMode: true}})
	if cmd == nil || a.config.generation <= previousGeneration {
		t.Fatal("profile did not schedule project refresh")
	}
	runProjectCommand(t, a, cmd)
	if a.config.WorkDir != project || a.config.InventoryPath != inv {
		t.Fatalf("profile context = %+v", a.config)
	}
	if a.inventory.Hosts["new-host"] == nil || a.inventory.Hosts["web-a"] != nil {
		t.Fatal("profile did not replace the displayed inventory")
	}
	if pb := a.pbPanel.SelectedPlaybook(); pb == nil || pb.Path != playbook {
		t.Fatalf("profile playbook = %+v", pb)
	}
	if a.pbPanel.CurrentLimit() != "" || len(a.pbPanel.SelectedTags()) != 0 || !a.pbPanel.CheckMode() {
		t.Fatal("profile did not replace complete draft options")
	}
	if a.pendingProfile != nil {
		t.Fatal("profile selection remained pending")
	}
}

func TestNarrowDashboardKeepsOperationOutcomeVisible(t *testing.T) {
	a := workbenchFixture(t)
	a.focused = core.PanelPlaybooks
	a.updateFocus()
	a.statusMsg = "Runtime update failed"
	if !strings.Contains(a.View(), a.statusMsg) {
		t.Fatal("80-column dashboard hides operation failures and completion status")
	}
}

func requestSelectedPlaybookReview(t *testing.T, a *App) {
	t.Helper()
	a.focused = core.PanelPlaybooks
	a.updateFocus()
	_, cmd := a.Update(overlayKey("r"))
	if cmd == nil {
		return
	}
	msg := cmd()
	if _, ok := msg.(panels.RunRequestMsg); !ok {
		t.Fatalf("unexpected run action message %T", msg)
	}
	a.Update(msg) // Never execute the preparation or run command.
}

func TestUnresolvedProfileCannotReviewUnrelatedPlaybook(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		a := workbenchFixture(t)
		oldPlaybook := a.pbPanel.SelectedPlaybook()
		_ = a.applyRunProfile(runprofiles.Profile{Name: "loading", WorkDir: t.TempDir(), Playbook: "deploy"})
		requestSelectedPlaybookReview(t, a)
		if a.mode == AppModeRunReview {
			t.Fatal("pending profile combined old selected playbook with new project context")
		}
		a.Update(panels.RunRequestMsg{Playbook: oldPlaybook})
		if a.mode == AppModeRunReview {
			t.Fatal("delayed RunRequest bypassed pending profile protection")
		}
	})
	t.Run("missing target", func(t *testing.T) {
		a := workbenchFixture(t)
		cmd := a.applyRunProfile(runprofiles.Profile{Name: "missing", WorkDir: a.config.WorkDir, Playbook: filepath.Join(a.config.WorkDir, "missing.yml")})
		runProjectCommand(t, a, cmd)
		requestSelectedPlaybookReview(t, a)
		if a.mode == AppModeRunReview {
			t.Fatal("unresolved profile left an unrelated playbook runnable")
		}
		// Explicitly inspect the visible playbook to accept a replacement target.
		_, inspect := a.Update(overlayKey("enter"))
		if inspect == nil {
			t.Fatal("explicit selection must remain available")
		}
		a.Update(inspect())
		a.Update(overlayKey("esc"))
		requestSelectedPlaybookReview(t, a)
		if a.mode != AppModeRunReview {
			t.Fatal("explicit selection did not unlock review")
		}
	})
}

func TestTargetedInspectorRevealsExactHost(t *testing.T) {
	for _, query := range []string{"", "other"} {
		t.Run("query="+query, func(t *testing.T) {
			a := workbenchFixture(t)
			w := a.workbench
			w.topic = "inventory"
			w.state().query = query
			a.Update(panels.InspectInventoryMsg{Host: "same"})
			a.Update(inspectionMsg{generation: w.generation, topic: "inventory", rows: []browserRow{
				{id: "group:same", label: "group same", target: "same"},
				{id: "host:same", label: "host same", target: "same"},
				{id: "host:other", label: "host other", target: "other"},
			}})
			if selected := w.selected(); selected == nil || selected.id != "host:same" {
				t.Fatalf("explicit host inspect selected %+v", selected)
			}
			if !w.state().detail {
				t.Fatal("explicit host inspection should open details on a narrow terminal")
			}
		})
	}
}

func TestInspectorDetailBottomRemainsReachableWithDiagnostics(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openWorkbench("inventory", "")
	w := a.workbench
	detail := strings.Repeat("variable value\n", 40) + "FINAL-INVENTORY-VALUE"
	a.Update(inspectionMsg{generation: w.generation, topic: "inventory", source: strings.Repeat("Plugin diagnostic\n", 12), rows: []browserRow{{id: "host:one", label: "host one", detail: detail}}})
	a.Update(overlayKey("enter"))
	_ = a.View()
	for i := 0; i < 100; i++ {
		a.Update(overlayKey("j"))
	}
	if !strings.Contains(a.View(), "FINAL-INVENTORY-VALUE") {
		t.Fatal("viewport clipping makes final inventory variables unreachable")
	}
}

func TestInspectorDetailSupportsVimEnds(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openWorkbench("inventory", "")
	w := a.workbench
	a.Update(inspectionMsg{generation: w.generation, topic: "inventory", rows: []browserRow{{id: "host:one", label: "host one", detail: strings.Repeat("value\n", 40) + "FINAL-VALUE"}}})
	a.Update(overlayKey("enter"))
	a.Update(overlayKey("G"))
	if !w.viewport.AtBottom() {
		t.Fatal("G does not scroll inspector detail to bottom")
	}
	a.Update(overlayKey("home"))
	if !w.viewport.AtTop() {
		t.Fatal("Home does not scroll inspector detail to top")
	}
}

func TestRunReviewLongFieldsRemainReadable(t *testing.T) {
	a := workbenchFixture(t)
	request := ansible.RunRequest{Kind: "playbook", Project: ansible.ProjectContext{WorkDir: a.config.WorkDir}, Playbook: a.pbPanel.SelectedPlaybook().Path}
	_ = a.prepareRun(request)
	a.Update(runPreparedMsg{id: a.reviewID, plan: ansible.RunPlan{Request: request, Preview: "ansible-playbook /" + strings.Repeat("nested-directory/", 20) + "FINAL-PLAYBOOK.yml"}})
	var observed strings.Builder
	for i := 0; i < 100; i++ {
		observed.WriteString(a.View())
		a.Update(overlayKey("j"))
	}
	if !strings.Contains(observed.String(), "FINAL-PLAYBOOK.yml") {
		t.Fatal("review clips the command suffix without an accessible scrolling path")
	}
}

func TestRuntimeCompletionCannotLosePostUpgradeVerification(t *testing.T) {
	a := workbenchFixture(t)
	a.runtimePending = true // A daily pre-upgrade observation is still running.
	a.runtimeID = 7
	oldID := a.runtimeID
	cancelled := false
	a.runtimeCancel = func() { cancelled = true }
	a.runtimeBusy = true
	_, afterUpgrade := a.Update(runtimeChangedMsg{result: ansible.Result{ExitCode: 0}})
	if afterUpgrade == nil || !cancelled || a.runtimeID == oldID {
		t.Fatal("completion did not replace pre-upgrade observation with fresh verification")
	}
	a.Update(runtimeObservedMsg{id: oldID, status: ansible.RuntimeStatus{CoreVersion: "old"}})
	if !a.runtimePending || a.workbench.runtime.CoreVersion == "old" {
		t.Fatal("stale pre-upgrade result replaced fresh pending verification")
	}
	a.Update(runtimeObservedMsg{id: a.runtimeID, status: ansible.RuntimeStatus{CoreVersion: "new"}})
	if a.runtimePending || a.workbench.runtime.CoreVersion != "new" {
		t.Fatal("current runtime verification was not accepted")
	}
}
