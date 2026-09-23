package ui

import (
	"context"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
)

type tagsSession struct {
	id, revision uint64
	target       string
	request      ansible.RunRequest
	origin       workspaceBookmark
	cancel       context.CancelFunc
}

func (a *App) openTagsDraft(prefill []string) tea.Cmd {
	a.syncExecutionDraft()
	pb := a.pbPanel.SelectedPlaybook()
	if pb == nil || a.pendingProfile != nil || a.profileNeedsSelection {
		a.statusMsg = "Select a playbook before choosing tags"
		return nil
	}
	if a.tagsSession.cancel != nil {
		a.tagsSession.cancel()
	}
	s := &a.tagsSession
	s.id++
	s.revision, s.target, s.request = a.draft.revision, a.draft.target, a.draft.request
	s.origin = a.bookmark()
	t := a.tagsOverlay
	t.SetTags(append([]string(nil), pb.Tags...))
	t.id, t.revision, t.target = s.id, s.revision, s.target
	t.title, t.notice, t.pending = filepath.Base(pb.Path)+" › Tags", "", true
	selection := a.draft.options.Tags
	if prefill != nil {
		selection = draftTags(prefill)
		t.notice = "Suggested declaration tags; Enter replaces current selection"
	}
	t.SetSelectedTags(selection)
	a.mode, a.focused = AppModeTagsBrowser, core.PanelLogs
	a.resizePanels()
	a.updateFocus()
	if a.runtimeBusy {
		t.pending = false
		t.notice = "Runtime updating; showing local and selected tags"
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	s.cancel = cancel
	id, revision, target, request, password := s.id, s.revision, s.target, s.request, a.vaultPassword
	a.observationJobs++
	return func() tea.Msg {
		defer cancel()
		plan, cleanup, err := prepareObservation(ctx, request, password)
		defer cleanup()
		result := ansible.TagCatalog{}
		if err == nil {
			result, err = ansible.DiscoverTags(ctx, plan)
		}
		return tagCatalogMsg{id, revision, target, result, err}
	}
}

func (a *App) closeTagsDraft() {
	if a.tagsSession.cancel != nil {
		a.tagsSession.cancel()
		a.tagsSession.cancel = nil
	}
	a.tagsSession.id++
	a.restoreBookmark(a.tagsSession.origin)
}
func (a *App) applyTags(msg TagsConfirmedMsg) {
	if a.mode != AppModeTagsBrowser || msg.ID != a.tagsSession.id || msg.Revision != a.draft.revision || msg.Target != a.draft.target {
		a.statusMsg = "Tag target changed; reopen Tags"
		if a.mode == AppModeTagsBrowser {
			a.closeTagsDraft()
		}
		return
	}
	a.draft.options.Tags = msg.Tags
	a.closeTagsDraft()
	a.statusMsg = "Tags applied · " + firstNonempty(msg.Tags, "No tag filter")
	a.syncExecutionDraft()
}
