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
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/history"
	"github.com/daviddwlee84/lazyansible/internal/runner"
	"github.com/daviddwlee84/lazyansible/internal/ui/panels"
	"github.com/daviddwlee84/lazyansible/internal/vault"
)

type runReview struct {
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
	if a.extraVarsRaw != "" {
		extra = append(extra, a.extraVarsRaw)
	}
	return ansible.RunRequest{Project: a.projectContext(), Check: a.pbPanel.CheckMode(), Diff: a.pbPanel.DiffMode(), ExtraVars: extra, Executable: a.config.Runtime.Executable}
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
	r := a.baseRequest()
	r.Kind = "playbook"
	r.Playbook = req.Playbook.Path
	r.Limit = req.Limit
	r.Tags = req.Tags
	r.Check = req.Check
	r.Diff = req.Diff
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
	r.Tags = strings.Join(a.pbPanel.SelectedTags(), ",")
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
	a.reviewID++
	id := a.reviewID
	origin := a.mode
	if origin == AppModeRunReview {
		origin = AppModeNormal
	}
	a.review = runReview{pending: true, origin: origin, viewport: viewport.New(max(1, a.width-4), max(1, a.height-7))}
	a.mode = AppModeRunReview
	ctx := a.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
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
	a.review = runReview{pending: true, origin: a.mode, runtimeAction: operation, viewport: viewport.New(max(1, a.width-4), max(1, a.height-7))}
	a.mode = AppModeRunReview
	opts := a.config.Runtime
	ctx := a.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
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
	if msg.err != nil {
		a.review.err = msg.err.Error()
		return nil
	}
	a.review.plan = &msg.plan
	p := msg.plan
	r := p.Request
	text := fmt.Sprintf("Operation: %s\nWorking directory: %s\nAnsible: %s\nExecutable: %s\nInventory: %s\nTarget: %s\nTags: %s\nCheck: %t  Diff: %t\n\n%s", r.Kind, firstNonempty(p.Command.Dir, r.Project.WorkDir), p.Runtime.CoreVersion, p.Command.Executable, firstNonempty(r.Project.Inventory, "Ansible default"), firstNonempty(r.Limit, r.Hosts, "playbook hosts / all"), r.Tags, r.Check, r.Diff, p.Preview)
	if a.review.runtimeAction != "" {
		text += "\n\nThis changes the shared uv tool used by your terminal too.\nExisting installation constraints and sources are retained."
	} else {
		text += "\n\nExecution overrides: stdout_callback=default; color disabled.\nExtra-vars and module argument values are hidden."
		if a.vaultPassword != "" {
			text += "\nA session Vault password will be supplied through a private temporary file."
		}
	}
	a.review.content = text
	a.reflowReview()
	return nil
}
func (a *App) updateReview(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "q":
			a.reviewID++
			a.mode = a.review.origin
			return nil
		case "tab", "shift+tab", "left", "right", "h", "l":
			a.review.confirm = !a.review.confirm
			return nil
		case "enter":
			if a.review.pending {
				return nil
			}
			if !a.review.confirm {
				a.reviewID++
				a.mode = a.review.origin
				return nil
			}
			if a.review.plan == nil || a.review.err != "" {
				return nil
			}
			if a.review.runtimeAction != "" {
				return a.executeRuntimePlan(*a.review.plan)
			}
			return a.executeRunPlan(*a.review.plan)
		}
	}
	var cmd tea.Cmd
	a.review.viewport, cmd = a.review.viewport.Update(msg)
	return cmd
}
func (a *App) reflowReview() {
	if a.review.content != "" {
		offset := a.review.viewport.YOffset
		a.review.viewport.SetContent(ansi.Hardwrap(a.review.content, max(1, a.review.viewport.Width), true))
		a.review.viewport.SetYOffset(offset)
	}
}
func (a *App) reviewView() string {
	state := "Review execution"
	if a.review.pending {
		state += " · preparing…"
	}
	body := a.review.viewport.View()
	if a.review.err != "" {
		body = "Cannot prepare this operation:\n" + a.review.err
	}
	buttons := "[ Cancel ]    Run"
	if a.review.confirm {
		buttons = "  Cancel    [ Run ]"
	}
	return fitScreen(state+"\n\n"+body+"\n\n"+buttons+"\nTab/←/→ choose · Enter confirm · j/k scroll · Esc back", max(1, a.width), max(1, a.height))
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
	a.statusMsg = "Running — see Logs"
	a.logsPanel.AddLine(core.LogLine{Text: "$ " + plan.Preview, Level: core.LogLevelCommand, Timestamp: time.Now()})
	r := plan.Request
	safe := r
	safe.Args = ""
	safe.ExtraVars = nil
	safe.VaultPasswordFile = ""
	safe.Env = nil
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
	password := a.vaultPassword
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
	a.focused = core.PanelLogs
	a.updateFocus()
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
