package ui

import (
	"path/filepath"
	"reflect"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/ui/panels"
)

// The App owns these options. Panels render the shared values and emit actions;
// asynchronous operations receive independent request snapshots.
type executionDraft struct {
	options        panels.ExecutionOptions
	request        ansible.RunRequest
	revision       uint64
	target, vault  string
	tagsByTarget   map[string]string
	keepTagsOnce   bool
	profileLoading bool
}

type workspaceBookmark struct {
	mode  AppMode
	panel core.Panel
	tab   workspaceTab
	zoom  bool
}

func (a *App) bookmark() workspaceBookmark {
	return workspaceBookmark{a.mode, a.focused, a.workspace.tab, a.workspace.zoom}
}
func (a *App) restoreBookmark(b workspaceBookmark) {
	a.mode, a.focused, a.workspace.tab, a.workspace.zoom = b.mode, b.panel, b.tab, b.zoom
	a.resizePanels()
	a.updateFocus()
}
func (a *App) draftTarget() string {
	pb := a.pbPanel.SelectedPlaybook()
	if pb == nil {
		return ""
	}
	path := pb.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.config.WorkDir, path)
	}
	return filepath.Clean(a.config.WorkDir) + "\x00" + filepath.Clean(path)
}

func (a *App) selectedRequest() ansible.RunRequest {
	r := a.baseRequest()
	r.Kind = "playbook"
	r.Limit = a.draft.options.Limit
	r.Tags = a.draft.options.Tags
	if pb := a.pbPanel.SelectedPlaybook(); pb != nil {
		r.Playbook = pb.Path
	}
	return r
}

// Never compare JSON requests: sensitive inputs intentionally have json:"-".
func (a *App) syncExecutionDraft() {
	if a.pbPanel == nil {
		return
	}
	d := &a.draft
	if d.tagsByTarget == nil {
		d.tagsByTarget = map[string]string{}
	}
	// A profile's options arrive before its asynchronous target discovery. Do
	// not attribute those options to the previously selected playbook.
	if a.pendingProfile != nil || (d.profileLoading && a.profileNeedsSelection) {
		d.profileLoading = true
		r := a.selectedRequest()
		if !reflect.DeepEqual(d.request, r) || d.vault != a.vaultPassword {
			d.request, d.vault = r, a.vaultPassword
			a.invalidateExecution("Profile target is loading")
		}
		return
	}
	target := a.draftTarget()
	if target != d.target {
		if d.target != "" && !d.profileLoading {
			d.tagsByTarget[d.target] = d.request.Tags
		}
		if d.revision > 0 && !d.keepTagsOnce && a.pendingProfile == nil {
			d.options.Tags = d.tagsByTarget[target]
		}
		d.target = target
	}
	d.profileLoading = false
	if a.pendingProfile == nil {
		d.keepTagsOnce = false
	}
	r := a.selectedRequest()
	if !reflect.DeepEqual(d.request, r) || d.vault != a.vaultPassword || d.revision == 0 {
		d.request, d.vault = r, a.vaultPassword
		a.invalidateExecution("Selection changed")
	}
	if target != "" {
		d.tagsByTarget[target] = d.options.Tags
	}
}

func (a *App) invalidateExecution(reason string) {
	a.draft.revision++
	p := &a.executionPreview
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.id++
	p.pending = false
	if p.result != nil {
		p.stale = true
	}
	if a.tagsSession.cancel != nil {
		a.tagsSession.cancel()
		a.tagsSession.cancel = nil
	}
	if a.mode == AppModeTagsBrowser {
		a.tagsOverlay.pending = false
		a.tagsOverlay.notice = reason + "; reopen Tags before applying"
	}
	if a.mode == AppModeRunReview && a.review.draftBound && a.review.revision != a.draft.revision {
		a.review.err = reason + "; return and review again"
		a.review.confirm = false
		a.syncReviewLayout()
	}
}

func (a *App) playbookSelectionChanged(old, selected *core.Playbook) {
	a.syncExecutionDraft()
}
func (a *App) toggleDraftCheck() {
	a.draft.options.Check = !a.draft.options.Check
	mode := "APPLY"
	if a.draft.options.Check {
		mode = "CHECK"
	}
	a.statusMsg = "Current playbook " + filepath.Base(a.selectedRequest().Playbook) + " · " + mode
}
func (a *App) toggleDraftDiff() {
	a.draft.options.Diff = !a.draft.options.Diff
	mode := "disabled"
	if a.draft.options.Diff {
		mode = "enabled"
	}
	a.statusMsg = "Current playbook " + filepath.Base(a.selectedRequest().Playbook) + " · Diff " + mode
}

func (a *App) reviewCurrentPlaybook() tea.Cmd {
	a.syncExecutionDraft()
	if a.pendingProfile != nil || a.profileNeedsSelection {
		a.statusMsg = "Select the profile's playbook before reviewing a run"
		return nil
	}
	if a.draft.request.Playbook == "" {
		a.statusMsg = "No playbook selected"
		return nil
	}
	return a.prepareRun(a.draft.request)
}

// A run's context must survive subsequent browsing or a newly selected project.
func (a *App) workspaceRequest() ansible.RunRequest {
	if a.mode == AppModeRunReview {
		return a.review.request
	}
	if a.mode == AppModeTagsBrowser {
		return a.tagsSession.request
	}
	if a.workspace.tab == workspaceLogs && a.logRequest != nil {
		return *a.logRequest
	}
	if a.workspace.tab == workspaceLogs && a.lastRunRequest != nil {
		return *a.lastRunRequest
	}
	return a.selectedRequest()
}

func draftTags(tags []string) string { return strings.Join(tags, ",") }
