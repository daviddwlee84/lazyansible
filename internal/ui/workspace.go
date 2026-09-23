package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
)

type workspaceTab uint8

const (
	workspaceLogs workspaceTab = iota
	workspaceRoles
	workspacePreview
)

// The existing fourth panel slot is the workspace. Tabs retain their own
// models and scroll; switching focus never creates another input reader.
type workspaceModel struct {
	tab  workspaceTab
	zoom bool
}

type workspaceRect struct{ x, y, width, height int }

func (r workspaceRect) contains(x, y int) bool {
	return r.width > 0 && r.height > 0 && x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

type workspaceLayout struct {
	header, main, feedback, footer workspaceRect
	top                            [3]workspaceRect
	workspace                      workspaceRect
	compact, workspaceVisible      bool
	framed                         bool
	innerWidth, innerHeight        int
	contextHeight, bodyHeight      int
	tabs                           [3]workspaceRect
}

var workspaceTabLabels = [...]string{"4 Logs", "O Roles", "p Preview"}

func (a *App) workspaceModal() bool {
	return a.mode == AppModeRunReview || a.mode == AppModeTagsBrowser || a.mode == AppModeRoles
}

// layout is the shared source of drawing, sizing and mouse-hit geometry.
func (a *App) layout() workspaceLayout {
	w, h := max(0, a.width), max(0, a.height)
	l := workspaceLayout{compact: w < 100 || h < 22}
	if w == 0 || h == 0 {
		return l
	}
	l.header = workspaceRect{0, 0, w, 1}
	tail := min(2, h-1)
	l.main = workspaceRect{0, 1, w, max(0, h-1-tail)}
	if tail > 0 {
		l.feedback = workspaceRect{0, h - tail, w, 1}
	}
	if tail > 1 {
		l.footer = workspaceRect{0, h - 1, w, 1}
	}
	l.workspace = l.main
	if l.compact || a.workspace.zoom {
		l.workspaceVisible = a.workspace.zoom || a.focused == core.PanelLogs || a.workspaceModal()
		if !l.workspaceVisible && int(a.focused) >= 0 && int(a.focused) < len(l.top) {
			l.top[int(a.focused)] = l.main
		}
	} else {
		topH := min(14, max(6, l.main.height/3))
		topH = min(topH, max(0, l.main.height-11))
		invW, pbW := w/4, w/4
		l.top = [3]workspaceRect{{0, 1, invW, topH}, {invW, 1, pbW, topH}, {invW + pbW, 1, w - invW - pbW, topH}}
		l.workspace = workspaceRect{0, 1 + topH, w, l.main.height - topH}
		l.workspaceVisible = true
	}
	l.framed = !l.compact && l.workspace.width >= 12 && l.workspace.height >= 5
	l.innerWidth, l.innerHeight = l.workspace.width, l.workspace.height
	if l.framed {
		l.innerWidth -= 4
		l.innerHeight -= 2
	}
	l.innerWidth, l.innerHeight = max(0, l.innerWidth), max(0, l.innerHeight)
	l.contextHeight = min(2, max(0, l.innerHeight-1))
	l.bodyHeight = max(0, l.innerHeight-1-l.contextHeight)
	if l.workspaceVisible && l.innerHeight > 0 {
		x, y := l.workspace.x, l.workspace.y
		if l.framed {
			x += 2
			y++
		}
		offset := 0
		for i, label := range workspaceTabLabels {
			width := min(lipgloss.Width(label)+2, max(0, l.innerWidth-offset))
			l.tabs[i] = workspaceRect{x + offset, y, width, 1}
			offset += lipgloss.Width(label) + 5
		}
	}
	return l
}

func (a *App) workspaceBodySize() (int, int) {
	l := a.layout()
	return l.innerWidth, l.bodyHeight
}

func (a *App) workspaceFocused() bool { return a.focused == core.PanelLogs }
func (a *App) workspaceLogsActive() bool {
	return a.workspaceFocused() && a.workspace.tab == workspaceLogs
}

func (a *App) workspaceInputActive() bool {
	if !a.workspaceFocused() {
		return false
	}
	switch a.workspace.tab {
	case workspaceLogs:
		return a.logsPanel.SearchActive()
	case workspaceRoles:
		return a.rolesWorkspaceTyping()
	case workspacePreview:
		return a.previewWorkspaceTyping()
	}
	return false
}

// openWorkspace only changes presentation. Preview observation stays an
// explicit p action; tab cycling must not start Ansible discovery by itself.
func (a *App) openWorkspace(tab workspaceTab) {
	a.workspace.tab = tab
	a.focused = core.PanelLogs
	a.updateFocus()
	a.resizePanels()
}

func (a *App) cycleWorkspace(direction int) tea.Cmd {
	next := workspaceTab((int(a.workspace.tab) + direction + 3) % 3)
	if next == workspaceRoles {
		return a.openRolesWorkspace()
	}
	a.openWorkspace(next)
	return nil
}

func (a *App) updateWorkspace(msg tea.Msg) tea.Cmd {
	switch a.workspace.tab {
	case workspaceRoles:
		return a.updateRolesWorkspace(msg)
	case workspacePreview:
		return a.updatePreviewWorkspace(msg)
	default:
		return a.logsPanel.Update(msg)
	}
}

func (a *App) workspaceTabBar() string {
	labels := append([]string(nil), workspaceTabLabels[:]...)
	active := a.workspace.tab
	if a.mode == AppModeRoles {
		active = workspaceRoles
	}
	for i, label := range labels {
		if workspaceTab(i) == active {
			labels[i] = overlaySelectedStyle.Render(" " + label + " ")
		} else {
			labels[i] = overlayLabelStyle.Render(" " + label + " ")
		}
	}
	return strings.Join(labels, overlayMutedStyle.Render(" │ "))
}

func (a *App) workspaceContextHeader() string {
	req := a.workspaceRequest()
	fromRun := a.mode == AppModeNormal && a.workspace.tab == workspaceLogs && a.lastRunRequest != nil
	return renderWorkspaceContext(req, fromRun)
}

func renderWorkspaceContext(req ansible.RunRequest, fromRun bool) string {
	kind, target := "Playbook", filepath.Base(req.Playbook)
	if req.Playbook == "" {
		target = "select a playbook"
	}
	if req.Kind == "role" {
		kind, target = "Standalone role", filepath.Base(req.RolePath)
	}
	if req.Kind == "adhoc" {
		kind, target = "Ad-hoc", req.Module
	}
	if req.Kind == "lint" {
		kind = "Lint"
	}
	if strings.HasPrefix(req.Kind, "runtime") {
		return overlayTitleStyle.Render("Runtime · "+plainTerminalLine(req.Kind)) + "\n" + overlayLabelStyle.Render("Shared Ansible tool · see operation output below")
	}
	if fromRun {
		kind = "Run: " + kind
	}
	limit := firstNonempty(req.Limit, req.Hosts, "playbook hosts")
	inv := "Ansible default"
	if req.Project.Inventory != "" {
		inv = filepath.Base(req.Project.Inventory)
	}
	line1 := overlayTitleStyle.Render(kind+" · "+plainTerminalLine(target)) + overlayLabelStyle.Render("  │ inventory "+plainTerminalLine(inv)+"  │ limit "+plainTerminalLine(limit))
	tags := firstNonempty(req.Tags, "All tags")
	mode := "APPLY"
	if req.Check {
		mode = "CHECK"
	}
	if req.Diff {
		mode += " +DIFF"
	}
	line2 := overlayItemStyle.Render("tags "+plainTerminalLine(tags)) + "  │ " + overlayActiveInputStyle.Render(mode) + overlayLabelStyle.Render("  │ cwd "+plainTerminalLine(req.Project.WorkDir))
	return line1 + "\n" + line2
}

func (a *App) workspaceContentView() string {
	switch a.mode {
	case AppModeRunReview:
		return a.reviewView()
	case AppModeTagsBrowser:
		return a.tagsOverlay.View()
	case AppModeRoles:
		return a.rolesWorkspaceView()
	}
	switch a.workspace.tab {
	case workspaceRoles:
		return a.rolesWorkspaceView()
	case workspacePreview:
		return a.previewWorkspaceView()
	default:
		return a.logsPanel.View()
	}
}

// fitWorkspace pads as well as clips so feedback and confirmation controls have
// stable rows even when a list is empty or a child view has only one line.
func fitWorkspace(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(fitScreen(content, width, height), "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		line = ansi.Truncate(line, width, "")
		lines[i] = line + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
	}
	return strings.Join(lines, "\n")
}

func (a *App) workspaceView() string {
	l := a.layout()
	if l.workspace.height <= 0 {
		return ""
	}
	parts := []string{fitWorkspace(a.workspaceTabBar(), l.innerWidth, 1)}
	if l.contextHeight > 0 {
		parts = append(parts, fitWorkspace(a.workspaceContextHeader(), l.innerWidth, l.contextHeight))
	}
	if l.bodyHeight > 0 {
		parts = append(parts, fitWorkspace(a.workspaceContentView(), l.innerWidth, l.bodyHeight))
	}
	content := fitWorkspace(strings.Join(parts, "\n"), l.innerWidth, l.innerHeight)
	if l.framed {
		content = a.wrapPanel(content, l.workspace.width, l.workspace.height, a.workspaceFocused() || a.workspaceModal(), "Workspace")
	}
	return fitWorkspace(content, l.workspace.width, l.workspace.height)
}

func (a *App) workspaceFeedback() string {
	state := plainTerminalLine(a.statusMsg)
	if a.running {
		state = "RUNNING · " + state
	}
	if a.runtimeBusy {
		state = "UPDATING ANSIBLE · " + state
	}
	return state
}

func (a *App) compactPanelView(panel core.Panel, rect workspaceRect) string {
	titles := []string{"1 Inventory (static preview)", "2 Playbooks", "3 Status"}
	views := []string{a.invPanel.View(), a.pbPanel.View(), a.statusPanel.View()}
	if int(panel) < 0 || int(panel) >= len(views) {
		return ""
	}
	contextRows := min(2, max(0, rect.height-2))
	parts := []string{overlayTitleStyle.Render(titles[int(panel)])}
	if contextRows > 0 {
		request := a.selectedRequest()
		fromRun := panel == core.PanelStatus && a.lastRunRequest != nil
		if fromRun {
			request = *a.lastRunRequest
		}
		parts = append(parts, fitWorkspace(renderWorkspaceContext(request, fromRun), rect.width, contextRows))
	}
	remaining := max(0, rect.height-1-contextRows)
	if remaining > 0 {
		parts = append(parts, fitWorkspace(views[int(panel)], rect.width, remaining))
	}
	return fitWorkspace(strings.Join(parts, "\n"), rect.width, rect.height)
}

func (a *App) workspaceTitle() string {
	return fmt.Sprintf("Workspace · %s", []string{"Logs", "Roles", "Preview"}[a.workspace.tab])
}
