package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/ui/panels"
)

func TestDraftTagsFollowPlaybookIdentity(t *testing.T) {
	a := workbenchFixture(t)
	first := a.pbPanel.SelectedPlaybook()
	second := &core.Playbook{Name: "other", Path: filepath.Join(a.config.WorkDir, "other.yml")}
	a.Update(playbooksLoadedMsg{pbs: []*core.Playbook{first, second}, generation: a.config.generation})
	a.pbPanel.SetActiveTags("imported-tag")
	a.Update(overlayKey("2"))
	a.Update(overlayKey("j"))
	if got := a.draft.request.Tags; got != "" {
		t.Fatalf("another playbook inherited tags: %q", got)
	}
	a.pbPanel.SetActiveTags("other-tag")
	a.Update(overlayKey("k"))
	if got := a.draft.request.Tags; got != "imported-tag" {
		t.Fatalf("first playbook lost tags: %q", got)
	}
	a.Update(overlayKey("j"))
	if got := a.draft.request.Tags; got != "other-tag" {
		t.Fatalf("second playbook lost tags: %q", got)
	}
}

func TestTagDraftCancelApplyAndUnknownSelections(t *testing.T) {
	a := workbenchFixture(t)
	a.pbPanel.SelectedPlaybook().Tags = []string{"greeting", "summary"}
	a.pbPanel.SetActiveTags("dynamic-tag")
	a.Update(overlayKey("2"))
	_ = a.openTagsDraft(nil) // Do not invoke Ansible in state tests.
	if got := a.tagsOverlay.SelectedTagsString(); got != "dynamic-tag" {
		t.Fatalf("unknown active tag disappeared: %q", got)
	}
	a.tagsOverlay.Update(overlayKey(" "))
	a.Update(overlayKey("esc"))
	if a.draft.request.Tags != "dynamic-tag" || a.focused != core.PanelPlaybooks {
		t.Fatal("cancel changed tags or lost focus")
	}
	_ = a.openTagsDraft(nil)
	a.tagsOverlay.Update(overlayKey(" "))
	confirm := a.tagsOverlay.Update(overlayKey("enter"))()
	a.Update(confirm)
	if a.draft.request.Tags != "greeting,dynamic-tag" || a.mode != AppModeNormal {
		t.Fatalf("apply=%q mode=%v", a.draft.request.Tags, a.mode)
	}
	// Async catalogue cannot replace an edited draft or remove a missing tag.
	_ = a.openTagsDraft(nil)
	s := a.tagsSession
	a.Update(tagCatalogMsg{id: s.id, revision: s.revision, target: s.target, result: ansible.TagCatalog{Parsed: true, Tags: []string{"role-child"}}})
	if a.tagsOverlay.SelectedTagsString() != "greeting,dynamic-tag" {
		t.Fatal("catalogue changed selected tags")
	}
	a.Update(overlayKey("esc"))
}

func TestLateTagConfirmationCannotChangeAnotherTarget(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openTagsDraft([]string{"greeting"})
	old := a.tagsOverlay.Update(overlayKey("enter"))()
	other := &core.Playbook{Name: "other", Path: filepath.Join(a.config.WorkDir, "other.yml")}
	a.Update(playbooksLoadedMsg{pbs: []*core.Playbook{other}, generation: a.config.generation})
	a.Update(old)
	if a.draft.request.Tags != "" || a.mode != AppModeNormal {
		t.Fatal("late Tags apply changed a new target")
	}
}

func TestPreviewRefreshIsExplicitAndRejectsLateResults(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openExecutionPreview()
	p := &a.executionPreview
	oldID, oldRevision := p.id, p.revision
	result := ansible.ExecutionPreview{Request: a.draft.request, Parsed: true, ObservedAt: time.Now(), Plays: []ansible.PreviewPlay{{Number: 1, Name: "fixture", Tasks: []ansible.PreviewTask{{Name: "old task"}}}}}
	a.Update(executionPreviewMsg{id: oldID, revision: oldRevision, result: result})
	if p.pending || p.result == nil || p.stale {
		t.Fatal("current preview not accepted")
	}
	_ = a.openExecutionPreview()
	oldID, oldRevision = p.id, p.revision
	cancelled := false
	originalCancel := p.cancel
	p.cancel = func() { cancelled = true; originalCancel() }
	_, cmd := a.Update(panels.SetLimitMsg{Limit: "new-host"})
	if cmd != nil || !p.stale || p.pending || !cancelled {
		t.Fatal("scope edit must cancel, mark stale, and not refresh automatically")
	}
	a.Update(executionPreviewMsg{id: oldID, revision: oldRevision, result: ansible.ExecutionPreview{Parsed: true, Output: "wrong"}})
	if p.result.Output == "wrong" || !p.stale {
		t.Fatal("late preview overwrote current observation")
	}
	_ = a.openExecutionPreview()
	id, revision := p.id, p.revision
	a.mode = AppModeHelp
	a.Update(executionPreviewMsg{id: id, revision: revision, result: result})
	if p.pending || p.stale || a.mode != AppModeHelp {
		t.Fatal("hidden valid preview was lost or stole focus")
	}
}

func TestSensitiveDraftChangesInvalidatePreviewAndReview(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openExecutionPreview()
	id, revision := a.executionPreview.id, a.draft.revision
	a.Update(ExtraVarsConfirmedMsg{Raw: "token=first"})
	if a.draft.revision == revision || a.executionPreview.id == id {
		t.Fatal("extra-vars change did not invalidate private request identity")
	}
	revision = a.draft.revision
	a.Update(VaultPasswordMsg{Password: "secret"})
	if a.draft.revision == revision {
		t.Fatal("Vault change ignored by request identity")
	}
	_ = a.reviewCurrentPlaybook()
	a.Update(runPreparedMsg{id: a.reviewID, plan: ansible.RunPlan{Request: a.draft.request, Preview: "ansible-playbook site.yml"}})
	a.Update(ExtraVarsConfirmedMsg{Raw: "token=second"})
	// A delayed input message may close its own overlay, but cannot execute the old plan.
	if a.review.revision == a.draft.revision {
		t.Fatal("changed secrets share review revision")
	}
}

func TestFailedPreviewKeepsRowsAndLatestDiagnostics(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openExecutionPreview()
	p := &a.executionPreview
	a.Update(executionPreviewMsg{id: p.id, revision: p.revision, result: ansible.ExecutionPreview{Parsed: true, Output: "previous output", Plays: []ansible.PreviewPlay{{Name: "previous play"}}}})
	_ = a.openExecutionPreview()
	a.Update(executionPreviewMsg{id: p.id, revision: p.revision, result: ansible.ExecutionPreview{ExitCode: 1, Diagnostics: "current error detail"}, err: errors.New("native failure")})
	if !p.stale || p.result.Plays[0].Name != "previous play" {
		t.Fatal("failed refresh destroyed useful rows")
	}
	p.section = 3
	a.syncPreviewWorkspaceLayout()
	content := a.previewDetailContent()
	if !strings.Contains(content, "current error detail") || strings.Contains(content, "previous output") {
		t.Fatalf("raw error surface showed old data: %s", content)
	}
}

func TestRunReviewRestoresWorkspaceAndRejectsDraftChanges(t *testing.T) {
	a := workbenchFixture(t)
	a.openWorkspace(workspacePreview)
	a.executionPreview.detail = true
	a.executionPreview.section = 2
	_ = a.reviewCurrentPlaybook()
	request := a.review.request
	a.Update(runPreparedMsg{id: a.reviewID, plan: ansible.RunPlan{Request: request, Preview: "ansible-playbook site.yml"}})
	a.Update(overlayKey("right"))
	a.Update(overlayKey("right"))
	if !a.review.confirm {
		t.Fatal("Right toggled away from Run")
	}
	a.Update(overlayKey("left"))
	a.Update(overlayKey("enter"))
	if a.mode != AppModeNormal || a.workspace.tab != workspacePreview || !a.executionPreview.detail || a.executionPreview.section != 2 {
		t.Fatal("review Cancel lost workspace state")
	}
	_ = a.reviewCurrentPlaybook()
	a.Update(runPreparedMsg{id: a.reviewID, plan: ansible.RunPlan{Request: request, Preview: "ansible-playbook site.yml"}})
	a.Update(panels.SetLimitMsg{Limit: "different"})
	a.Update(overlayKey("right"))
	_, cmd := a.Update(overlayKey("enter"))
	if cmd != nil || a.running || a.review.err == "" {
		t.Fatal("stale review executed")
	}
}

func TestRunHostInspectorUsesFrozenInventoryEvenAfterFocusChanges(t *testing.T) {
	a := workbenchFixture(t)
	r := a.selectedRequest()
	r.Project.Inventory = "run-inventory.ini"
	a.lastRunRequest = &r
	a.config.InventoryPath = "new-inventory.ini"
	a.focused = core.PanelPlaybooks
	_, _ = a.Update(panels.InspectInventoryMsg{Host: "web-a", FromRun: true})
	if a.workbench.projectOverride == nil || a.workbench.projectOverride.Inventory != "run-inventory.ini" {
		t.Fatal("status host inspected using newly selected inventory")
	}
}

func TestPreviewViewportKeepsEndsAndResizeReachable(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openExecutionPreview()
	p := &a.executionPreview
	result := ansible.ExecutionPreview{Parsed: true, Output: strings.Repeat("line\n", 100) + "FINAL-OUTPUT"}
	a.Update(executionPreviewMsg{id: p.id, revision: p.revision, result: result})
	p.section = 3
	p.detail = true
	a.syncPreviewWorkspaceLayout()
	a.Update(overlayKey("G"))
	if !strings.Contains(a.View(), "FINAL-OUTPUT") {
		t.Fatal("preview bottom unreachable")
	}
	a.Update(tea.WindowSizeMsg{Width: 35, Height: 12})
	a.Update(overlayKey("G"))
	if !strings.Contains(a.View(), "FINAL-OUTPUT") {
		t.Fatal("resized preview bottom unreachable")
	}
}

func TestObservedExternalRuntimeChangeInvalidatesPreview(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openExecutionPreview()
	p := &a.executionPreview
	old := ansible.RuntimeStatus{Executable: "/tool/ansible", CoreVersion: "2.20.5"}
	a.Update(executionPreviewMsg{id: p.id, revision: p.revision, result: ansible.ExecutionPreview{Parsed: true, Runtime: old}})
	a.runtimeID++
	a.Update(runtimeObservedMsg{id: a.runtimeID, status: old})
	if p.stale {
		t.Fatal("identical runtime invalidated preview")
	}
	a.runtimeID++
	a.Update(runtimeObservedMsg{id: a.runtimeID, status: ansible.RuntimeStatus{Executable: old.Executable, CoreVersion: "2.21.4"}})
	if !p.stale {
		t.Fatal("external runtime upgrade left preview fresh")
	}
}

func TestQuitWaitsForCancelledObservationCleanupAndHistory(t *testing.T) {
	a := workbenchFixture(t)
	_ = a.openExecutionPreview()
	id, revision := a.executionPreview.id, a.executionPreview.revision
	a.historyJobs = 1
	if cmd := a.requestQuit(); cmd != nil {
		t.Fatal("quit did not wait for observation cleanup")
	}
	if !a.quitting || a.ctx.Err() == nil {
		t.Fatal("quit did not cancel work")
	}
	_, cmd := a.Update(executionPreviewMsg{id: id, revision: revision, err: errors.New("cancelled")})
	if cmd != nil || a.observationJobs != 0 {
		t.Fatal("observation completion ignored or exited before history")
	}
	_, cmd = a.Update(historySavedMsg{})
	if cmd == nil {
		t.Fatal("cleanup completion did not exit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("final cleanup did not return Quit")
	}
}

func TestWideStatusRetainsRunContextAcrossWorkspaceAndRuntimeChanges(t *testing.T) {
	a := workbenchFixture(t)
	r := a.selectedRequest()
	r.Playbook = filepath.Join(a.config.WorkDir, "completed.yml")
	r.Project.Inventory = "staging.ini"
	a.lastRunRequest = &r
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 35})
	a.openWorkspace(workspacePreview)
	if view := a.View(); !strings.Contains(view, "completed.yml") || !strings.Contains(view, "staging.ini") {
		t.Fatal("wide Status lacks its frozen run context")
	}
	_ = a.executeRuntimePlan(ansible.RunPlan{Request: ansible.RunRequest{Kind: "runtime-upgrade"}})
	if a.lastRunRequest.Playbook != r.Playbook || a.workspaceRequest().Kind != "runtime-upgrade" {
		t.Fatal("runtime logs overwrote the retained playbook result context")
	}
}
