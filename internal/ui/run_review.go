package ui

import (
	"context"
	"encoding/json"
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
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/history"
	"github.com/daviddwlee84/lazyansible/internal/runner"
	"github.com/daviddwlee84/lazyansible/internal/ui/panels"
	"github.com/daviddwlee84/lazyansible/internal/vault"
)

type runReview struct {
	password         string
	request          ansible.RunRequest
	revision         uint64
	draftBound       bool
	bookmark         workspaceBookmark
	cancel           context.CancelFunc
	content          string
	plan             *ansible.RunPlan
	pending, confirm bool
	err              string
	origin           AppMode
	runtimeAction    string
	viewport         viewport.Model
}
type runPreparedMsg struct {
	id   uint64
	plan ansible.RunPlan
	err  error
}
type historySavedMsg struct{ err error }

func (a *App) baseRequest() ansible.RunRequest {
	extra := []string{}
	if a.sshExtraVars != "" {
		extra = append(extra, a.sshExtraVars)
	}
	if a.draft.options.ExtraVars != "" {
		extra = append(extra, a.draft.options.ExtraVars)
	}
	return ansible.RunRequest{Project: a.projectContext(), Check: a.draft.options.Check, Diff: a.draft.options.Diff, ExtraVars: extra, Executable: a.config.Runtime.Executable}
}
func (a *App) startRun(req panels.RunRequestMsg) tea.Cmd {
	if a.pendingProfile != nil || a.profileNeedsSelection {
		a.statusMsg = "Select the profile's playbook before reviewing a run"
		return nil
	}
	if req.Playbook == nil {
		a.statusMsg = "No playbook selected"
		return nil
	}
	a.syncExecutionDraft()
	r := a.draft.request
	if req.Playbook.Path != r.Playbook || req.Tags != r.Tags || req.Limit != r.Limit || req.Check != r.Check || req.Diff != r.Diff {
		a.statusMsg = "Selection changed; review the current playbook again"
		return nil
	}
	return a.prepareRun(r)
}
func (a *App) startAdHoc(opts core.AdHocOptions) tea.Cmd {
	r := a.baseRequest()
	r.Kind = "adhoc"
	r.Hosts = opts.Hosts
	r.Module = opts.Module
	r.Args = opts.Args
	r.Become = opts.Become
	r.Project.Inventory = opts.Inventory
	if len(opts.ExtraVars) > 0 {
		b, _ := json.Marshal(opts.ExtraVars)
		r.ExtraVars = append(r.ExtraVars, string(b))
	}
	return a.prepareRun(r)
}
func (a *App) startRoleRun(req RoleRunMsg) tea.Cmd {
	r := a.baseRequest()
	r.Kind = "role"
	r.RolePath = req.RolePath
	r.Project.Inventory = req.Inventory
	r.Limit = req.Limit
	r.Tags = req.Tags
	return a.prepareRun(r)
}
func (a *App) startRunFromHistory(rec *history.Record) tea.Cmd {
	if rec == nil {
		return nil
	}
	if rec.RequiresInput {
		a.statusMsg = "History omits arguments, extra-vars or Vault input; configure these again before running"
		return nil
	}
	if rec.Request == nil || rec.Request.Project.WorkDir == "" {
		a.statusMsg = "This legacy record lacks a complete project/run context; inspect it and configure a new run"
		return nil
	}
	r := *rec.Request
	r.Executable = a.config.Runtime.Executable
	r.Project.Executable = a.config.Runtime.Executable
	return a.prepareRun(r)
}
func (a *App) prepareRun(req ansible.RunRequest) tea.Cmd {
	if a.running || a.linting || a.runtimeBusy {
		a.statusMsg = "Wait for the current operation to finish"
		return nil
	}
	if a.review.cancel != nil {
		a.review.cancel()
	}
	a.reviewID++
	id := a.reviewID
	origin := a.mode
	if origin == AppModeRunReview {
		origin = AppModeNormal
	}
	bookmark := a.bookmark()
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	a.review = runReview{pending: true, origin: origin, bookmark: bookmark, request: req, password: a.vaultPassword, revision: a.draft.revision, draftBound: req.Kind == "playbook" && req.Playbook == a.draft.request.Playbook, cancel: cancel, viewport: viewport.New(1, 1)}
	a.mode = AppModeRunReview
	a.resizePanels()
	return func() tea.Msg {
		defer cancel()
		plan, err := ansible.Prepare(ctx, req)
		return runPreparedMsg{id: id, plan: plan, err: err}
	}
}
func (a *App) reviewRuntime(operation string) tea.Cmd {
	if a.running || a.linting || a.runtimeBusy {
		a.statusMsg = "Wait for the current operation before changing the shared runtime"
		a.workbench.err = a.statusMsg
		return nil
	}
	a.reviewID++
	id := a.reviewID
	bookmark := a.bookmark()
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	a.review = runReview{pending: true, origin: a.mode, bookmark: bookmark, runtimeAction: operation, cancel: cancel, viewport: viewport.New(1, 1)}
	a.mode = AppModeRunReview
	a.resizePanels()
	opts := a.config.Runtime
	return func() tea.Msg {
		defer cancel()
		var plan ansible.RunPlan
		var err error
		if operation == "upgrade" {
			plan, err = ansible.PrepareUpgrade(ctx, opts)
		} else {
			plan, err = ansible.PrepareInstall(ctx, opts, "")
		}
		return runPreparedMsg{id: id, plan: plan, err: err}
	}
}
func (a *App) acceptRunPlan(msg runPreparedMsg) tea.Cmd {
	if msg.id != a.reviewID || a.mode != AppModeRunReview {
		return nil
	}
	a.review.pending = false
	a.review.cancel = nil
	if a.review.draftBound && a.review.revision != a.draft.revision {
		a.review.err = "Selection changed; return and review again"
		a.syncReviewLayout()
		return nil
	}
	if msg.err != nil {
		a.review.err = msg.err.Error()
		a.syncReviewLayout()
		return nil
	}
	a.review.plan = &msg.plan
	p := msg.plan
	r := p.Request
	a.review.request = r
	field := func(label, value string) string {
		return overlayLabelStyle.Render(label+": ") + overlayItemStyle.Render(plainTerminalLine(value))
	}
	text := strings.Join([]string{
		overlayTitleStyle.Render("Execution scope"),
		field("Operation", r.Kind),
		field("Working directory", firstNonempty(p.Command.Dir, r.Project.WorkDir)),
		field("Inventory", firstNonempty(r.Project.Inventory, "Ansible default")),
		field("Target", firstNonempty(r.Limit, r.Hosts, "playbook hosts / all")),
		field("Tags", firstNonempty(r.Tags, "No tag filter")),
		field("Mode", fmt.Sprintf("Check: %t · Diff: %t", r.Check, r.Diff)),
		"", overlayTitleStyle.Render("Command"), overlayItemStyle.Render(plainTerminalText(p.Preview)),
		"", overlayTitleStyle.Render("Ansible runtime"),
		field("Core version", p.Runtime.CoreVersion), field("Executable", p.Command.Executable),
	}, "\n")
	if a.review.runtimeAction != "" {
		text += "\n\nThis changes the shared uv tool used by your terminal too.\nExisting installation constraints and sources are retained."
	} else {
		text += "\n\nExtra-vars and credential values are hidden."
		if a.review.password != "" {
			text += "\nA session Vault password will be supplied through a private temporary file."
		}
	}
	if r.Kind == "role" {
		text = "STANDALONE ROLE — new generated play\nDoes not inherit the parent playbook's vars, pre_tasks, handlers or execution order.\n\n" + text
	}
	if r.Check {
		text = "CHECK MODE — supported modules predict changes; tasks may override check mode.\n\n" + text
	}
	a.review.content = text
	a.syncReviewLayout()
	return nil
}
func (a *App) closeRunReview() {
	if a.review.cancel != nil {
		a.review.cancel()
		a.review.cancel = nil
	}
	a.reviewID++
	a.restoreBookmark(a.review.bookmark)
	a.mode = a.review.origin
}
func (a *App) updateReview(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "q":
			a.closeRunReview()
			return nil
		case "tab", "shift+tab":
			a.review.confirm = !a.review.confirm
			return nil
		case "left", "h":
			a.review.confirm = false
			return nil
		case "right", "l":
			a.review.confirm = true
			return nil
		case "enter":
			_, h := a.workspaceBodySize()
			if a.review.pending || h < 4 {
				return nil
			}
			if !a.review.confirm {
				a.closeRunReview()
				return nil
			}
			if a.review.plan == nil || a.review.err != "" {
				return nil
			}
			if a.review.draftBound && a.review.revision != a.draft.revision {
				return nil
			}
			if a.review.runtimeAction != "" {
				return a.executeRuntimePlan(*a.review.plan)
			}
			return a.executeRunPlan(*a.review.plan)
		case "g", "home":
			a.review.viewport.GotoTop()
			return nil
		case "G", "end":
			a.review.viewport.GotoBottom()
			return nil
		}
	}
	var cmd tea.Cmd
	a.review.viewport, cmd = a.review.viewport.Update(msg)
	return cmd
}
func (a *App) syncReviewLayout() {
	w, h := a.workspaceBodySize()
	a.review.viewport.Width, a.review.viewport.Height = max(1, w), max(1, h-3)
	a.reflowReview()
}
func (a *App) reflowReview() {
	offset := a.review.viewport.YOffset
	content := a.review.content
	if a.review.err != "" {
		content = "Cannot prepare this operation:\n" + plainTerminalText(a.review.err)
	}
	a.review.viewport.SetContent(ansi.Hardwrap(content, max(1, a.review.viewport.Width), true))
	a.review.viewport.SetYOffset(offset)
}
func (a *App) reviewView() string {
	w, h := a.workspaceBodySize()
	if w <= 0 || h <= 0 {
		return ""
	}
	if h < 4 {
		return fitScreen("Review · enlarge terminal · Esc back", w, h)
	}
	title := "Review execution"
	if a.review.pending {
		title += " · preparing…"
	}
	if a.review.request.Check {
		title = "Review CHECK execution"
	}
	if a.review.request.Kind == "role" {
		title = "Review standalone role"
	}
	if a.review.runtimeAction != "" {
		title = "Review shared runtime " + a.review.runtimeAction
	}
	runLabel := "Run"
	if a.review.request.Check {
		runLabel = "Run check"
	}
	cancelButton, runButton := "  Cancel  ", "  "+runLabel+"  "
	active := lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Background(colorBorderFocus)
	if a.review.confirm {
		runButton = active.Render("[ " + runLabel + " ]")
	} else {
		cancelButton = active.Render("[ Cancel ]")
	}
	buttons := cancelButton + "    " + runButton
	return fitScreen(overlayTitleStyle.Render(plainTerminalLine(title)), w, 1) + "\n" + fitScreen(a.review.viewport.View(), w, h-3) + "\n" + fitScreen(buttons, w, 1) + "\n" + fitScreen("Tab choose · Enter confirm · j/k scroll · Esc back", w, 1)
}
func (a *App) executeRunPlan(plan ansible.RunPlan) tea.Cmd {
	if a.running || a.linting || a.runtimeBusy {
		return nil
	}
	a.running = true
	a.retryHosts = nil
	a.statusPanel.Reset()
	a.statusPanel.SetRunning(true)
	a.logsPanel.Clear()
	a.mode = AppModeNormal
	a.openWorkspace(workspaceLogs)
	a.statusMsg = "Running — see Logs"
	a.logsPanel.AddLine(core.LogLine{Text: "$ " + plan.Preview, Level: core.LogLevelCommand, Timestamp: time.Now()})
	r := plan.Request
	safe := r
	safe.Args = ""
	safe.ExtraVars = nil
	safe.VaultPasswordFile = ""
	safe.Env = nil
	a.lastRunRequest = &safe
	a.logRequest = &safe
	a.resizePanels()
	name := filepath.Base(r.Playbook)
	if r.Kind == "role" {
		name = "role:" + filepath.Base(r.RolePath)
	}
	if r.Kind == "adhoc" {
		name = r.Module
	}
	a.runRecord = &history.Record{ID: fmt.Sprint(time.Now().UnixNano()), Kind: r.Kind, PlaybookName: name, PlaybookPath: r.Playbook, Inventory: r.Project.Inventory, Limit: r.Limit, Tags: r.Tags, CheckMode: r.Check, DiffMode: r.Diff, Module: r.Module, WorkDir: r.Project.WorkDir, RolePath: r.RolePath, Request: &safe, RequiresInput: r.Args != "" || len(r.ExtraVars) > 0 || r.VaultPasswordFile != "" || a.vaultPassword != "", StartTime: time.Now()}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancelRun = cancel
	password := a.review.password
	send := func(msg tea.Msg) {
		if a.program != nil {
			a.program.Send(msg)
		}
	}
	return func() tea.Msg {
		if password != "" {
			path, err := vault.WriteTempPassword(password)
			if err != nil {
				return runner.RunFinishedMsg{ExitCode: -1, Err: err}
			}
			defer os.Remove(path)
			plan.Command.Args = append(append([]string{}, plan.Command.Args...), "--vault-password-file", path)
		}
		return runner.StreamPlanCmd(ctx, plan, send)()
	}
}
func (a *App) executeRuntimePlan(plan ansible.RunPlan) tea.Cmd {
	if a.running || a.linting || a.runtimeBusy {
		return nil
	}
	if a.runtimeCancel != nil {
		a.runtimeCancel()
	}
	a.runtimeID++
	a.runtimePending = false
	a.runtimeBusy = true
	a.mode = AppModeNormal
	logRequest := plan.Request
	logRequest.Project.WorkDir = firstNonempty(plan.Command.Dir, a.config.WorkDir)
	a.logRequest = &logRequest
	a.openWorkspace(workspaceLogs)
	a.logsPanel.Clear()
	a.statusMsg = "Updating shared Ansible runtime…"
	a.logsPanel.AddLine(core.LogLine{Text: "$ " + plan.Preview, Level: core.LogLevelCommand, Timestamp: time.Now()})
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancelRun = cancel
	return func() tea.Msg {
		defer cancel()
		result, err := ansible.Execute(ctx, plan, func(event ansible.Event) {
			if a.program != nil {
				a.program.Send(runner.LogMsg{Line: core.LogLine{Text: event.Line, Timestamp: time.Now()}})
			}
		})
		return runtimeChangedMsg{result: result, err: err}
	}
}
