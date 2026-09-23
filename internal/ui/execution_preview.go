package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/vault"
)

type executionPreviewModel struct {
	id, revision                    uint64
	cancel                          context.CancelFunc
	pending, stale, detail          bool
	err                             string
	result                          *ansible.ExecutionPreview
	failedResult                    *ansible.ExecutionPreview
	section                         int
	offsets                         [4]int
	viewport                        viewport.Model
	initialized                     bool
	renderedResult, renderedFailure *ansible.ExecutionPreview
	renderedSection                 int
	renderedError                   string
}
type executionPreviewMsg struct {
	id, revision uint64
	result       ansible.ExecutionPreview
	err          error
}
type tagCatalogMsg struct {
	id, revision uint64
	target       string
	result       ansible.TagCatalog
	err          error
}

// Session credentials follow the same private temporary-file lifecycle for
// observations and runs. In-memory request values are never displayed.
func prepareObservation(ctx context.Context, request ansible.RunRequest, password string) (ansible.RunPlan, func(), error) {
	cleanup := func() {}
	if password != "" {
		path, err := vault.WriteTempPassword(password)
		if err != nil {
			return ansible.RunPlan{}, cleanup, err
		}
		cleanup = func() { _ = os.Remove(path) }
		request.VaultPasswordFile = path
	}
	plan, err := ansible.Prepare(ctx, request)
	return plan, cleanup, err
}

func (a *App) openExecutionPreview() tea.Cmd {
	a.syncExecutionDraft()
	a.openWorkspace(workspacePreview)
	if a.draft.request.Playbook == "" || a.pendingProfile != nil || a.profileNeedsSelection {
		a.statusMsg = "Select a playbook before previewing"
		return nil
	}
	if a.runtimeBusy {
		a.statusMsg = "Wait for the Ansible runtime update"
		return nil
	}
	p := &a.executionPreview
	if !p.initialized {
		p.section = 1
		p.viewport = viewport.New(1, 1)
		p.initialized = true
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.id++
	p.revision = a.draft.revision
	p.pending, p.err = true, ""
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	p.cancel = cancel
	id, revision := p.id, p.revision
	request, password := a.draft.request, a.vaultPassword
	a.syncPreviewWorkspaceLayout()
	a.observationJobs++
	return func() tea.Msg {
		defer cancel()
		plan, cleanup, err := prepareObservation(ctx, request, password)
		defer cleanup()
		result := ansible.ExecutionPreview{}
		if err == nil {
			result, err = ansible.Preview(ctx, plan)
		}
		return executionPreviewMsg{id, revision, result, err}
	}
}

func (a *App) acceptExecutionObservation(msg tea.Msg) bool {
	switch m := msg.(type) {
	case executionPreviewMsg:
		if a.observationJobs > 0 {
			a.observationJobs--
		}
		p := &a.executionPreview
		if m.id != p.id || m.revision != a.draft.revision {
			return true
		}
		p.pending, p.cancel = false, nil
		p.err = ""
		if m.err != nil {
			p.err = m.err.Error()
			p.failedResult = &m.result
			// Preserve useful previous rows, but never label them fresh.
			if p.result != nil {
				p.stale = true
			} else {
				p.result = &m.result
			}
		} else {
			p.result, p.stale = &m.result, false
			p.failedResult = nil
		}
		a.syncPreviewWorkspaceLayout()
		return true
	case tagCatalogMsg:
		if a.observationJobs > 0 {
			a.observationJobs--
		}
		s := &a.tagsSession
		if a.mode != AppModeTagsBrowser || m.id != s.id || m.revision != a.draft.revision || m.target != a.draft.target {
			return true
		}
		s.cancel = nil
		a.tagsOverlay.pending = false
		if m.err != nil {
			a.tagsOverlay.notice = "Ansible discovery failed: " + m.err.Error()
		} else if !m.result.Parsed {
			a.tagsOverlay.notice = "Ansible tag format unavailable; showing local and selected tags"
		} else {
			a.tagsOverlay.MergeTags(m.result.Tags, "Ansible")
			a.tagsOverlay.notice = "Ansible tags under current config; dynamic includes may add more"
			if m.result.Diagnostics != "" {
				a.tagsOverlay.notice += " · " + m.result.Diagnostics
			}
		}
		return true
	}
	return false
}

func (a *App) previewWorkspaceTyping() bool { return false }

var previewSections = []string{"Hosts", "Tasks", "Tags", "Ansible output"}

func (a *App) updatePreviewWorkspace(msg tea.Msg) tea.Cmd {
	p := &a.executionPreview
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "h", "left", "esc":
		p.detail = false
	case "l", "right", "enter":
		p.detail = true
	default:
		if !p.detail {
			old := p.section
			switch key.String() {
			case "j", "down":
				p.section = min(3, p.section+1)
			case "k", "up":
				p.section = max(0, p.section-1)
			case "g", "home":
				p.section = 0
			case "G", "end":
				p.section = 3
			}
			if old != p.section {
				p.offsets[old] = p.viewport.YOffset
			}
		} else {
			switch key.String() {
			case "g", "home":
				p.viewport.GotoTop()
			case "G", "end":
				p.viewport.GotoBottom()
			default:
				p.viewport, _ = p.viewport.Update(msg)
			}
			p.offsets[p.section] = p.viewport.YOffset
		}
	}
	a.syncPreviewWorkspaceLayout()
	return nil
}

func (a *App) previewDetailContent() string {
	p := &a.executionPreview
	if p.section == 3 {
		var text strings.Builder
		if p.err != "" {
			text.WriteString("Observation failed: " + p.err + "\n\n")
		}
		result := p.result
		if p.failedResult != nil {
			result = p.failedResult
		}
		if result != nil {
			text.WriteString(result.Command + "\n\n" + result.Output)
			if result.Diagnostics != "" {
				text.WriteString("\nDiagnostics:\n" + result.Diagnostics)
			}
		}
		if text.Len() == 0 {
			return "No observation yet. Press p to query Ansible."
		}
		return plainTerminalText(text.String())
	}
	if p.result == nil {
		return "Press p to list the selected playbook's scope.\nThis does not execute its tasks."
	}
	if !p.result.Parsed || p.result.ExitCode != 0 {
		return "Structured observation unavailable.\nSee Ansible output for results and diagnostics."
	}
	var text strings.Builder
	for _, play := range p.result.Plays {
		text.WriteString(fmt.Sprintf("Play %d · %s\n", play.Number, firstNonempty(play.Name, play.Pattern)))
		switch p.section {
		case 0:
			text.WriteString("Pattern: " + play.Pattern + "\n")
			if len(play.Hosts) == 0 {
				text.WriteString("No matched hosts.\n")
			}
			for _, host := range play.Hosts {
				text.WriteString("  " + host + "\n")
			}
		case 1:
			if len(play.Tasks) == 0 {
				text.WriteString("No tasks listed for this selection.\n")
			}
			for i, task := range play.Tasks {
				text.WriteString(fmt.Sprintf("%d. %s", i+1, task.Name))
				if len(task.Tags) > 0 {
					text.WriteString("  [" + strings.Join(task.Tags, ", ") + "]")
				}
				text.WriteString("\n")
			}
		case 2:
			if len(play.Tags) == 0 {
				text.WriteString("No tags listed for this selection.\n")
			}
			for _, tag := range play.Tags {
				text.WriteString("  " + tag + "\n")
			}
		}
		text.WriteString("\n")
	}
	if len(p.result.Plays) == 0 {
		text.WriteString("No plays listed for this selection.")
	}
	return plainTerminalText(text.String())
}

func (a *App) syncPreviewWorkspaceLayout() {
	p := &a.executionPreview
	if !p.initialized {
		p.section = 1
		p.viewport = viewport.New(1, 1)
		p.initialized = true
	}
	w, h := a.workspaceBodySize()
	if w >= 64 {
		w -= 19
	}
	w, h = max(1, w), max(1, h-4)
	if p.viewport.Width == w && p.viewport.Height == h && p.renderedResult == p.result && p.renderedFailure == p.failedResult && p.renderedSection == p.section && p.renderedError == p.err {
		return
	}
	p.renderedResult, p.renderedFailure, p.renderedSection, p.renderedError = p.result, p.failedResult, p.section, p.err
	p.viewport.Width, p.viewport.Height = w, h
	p.viewport.SetContent(ansi.Hardwrap(a.previewDetailContent(), max(1, w), true))
	p.viewport.SetYOffset(p.offsets[p.section])
}

func (a *App) previewWorkspaceView() string {
	w, h := a.workspaceBodySize()
	if w <= 0 || h <= 0 {
		return ""
	}
	p := &a.executionPreview
	state := "Not observed · p to preview"
	if p.result != nil {
		state = "Observed " + p.result.ObservedAt.Format("15:04:05")
	}
	if p.stale {
		state = "STALE · selection changed · p refresh"
	}
	if p.pending {
		state = "Loading Ansible scope…"
	}
	if p.err != "" {
		state = "Preview failed · p retry · see Ansible output"
	}
	label := "Static lists; dynamic includes and conditions may differ at execution."
	if p.stale && p.result != nil {
		label = "Previous: " + filepath.Base(p.result.Request.Playbook) + " · tags " + firstNonempty(p.result.Request.Tags, "unfiltered")
	}
	var rows []string
	visibleRows := max(1, h-4)
	start := max(0, p.section-visibleRows+1)
	end := min(len(previewSections), start+visibleRows)
	for i := start; i < end; i++ {
		name := previewSections[i]
		if i == p.section {
			rows = append(rows, overlaySelectedStyle.Render("> "+name))
		} else {
			rows = append(rows, "  "+name)
		}
	}
	list := strings.Join(rows, "\n")
	body := list
	if w >= 64 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, fitScreen(list, 19, max(1, h-4)), p.viewport.View())
	} else if p.detail {
		body = p.viewport.View()
	}
	foot := "j/k select · Enter/l details · h/Esc sections · p refresh"
	if p.detail {
		foot = "j/k scroll · g/G ends · h/Esc sections · r review"
	}
	return fitScreen(overlayTitleStyle.Render(previewSections[p.section]+" · "+state)+"\n"+ansi.Truncate(plainTerminalLine(label), w, "…")+"\n"+fitScreen(body, w, max(1, h-4))+"\n"+ansi.Truncate(foot, w, "…"), w, h)
}
