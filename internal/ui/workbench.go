package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	appcfg "github.com/daviddwlee84/lazyansible/internal/config"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/paths"
)

type browserRow struct{ id, label, detail, target string }
type browserState struct {
	rows   []browserRow
	cursor int
	query  string
	detail bool
}
type workbenchModel struct {
	topic               string
	states              map[string]*browserState
	input               textinput.Model
	filtering           bool
	viewport            viewport.Model
	generation          uint64
	cancel              context.CancelFunc
	pending             bool
	err, source, target string
	runtime             ansible.RuntimeStatus
	update              ansible.UpdateStatus
	projectOverride     *ansible.ProjectContext
}

func newWorkbench() *workbenchModel {
	input := textinput.New()
	input.Prompt = "/ "
	input.CharLimit = 200
	return &workbenchModel{states: map[string]*browserState{}, input: input, viewport: viewport.New(40, 12)}
}
func (w *workbenchModel) state() *browserState {
	if w.states[w.topic] == nil {
		w.states[w.topic] = &browserState{}
	}
	return w.states[w.topic]
}
func (w *workbenchModel) visible() []browserRow {
	rows := w.state().rows
	query := strings.ToLower(w.state().query)
	if query == "" {
		return rows
	}
	out := []browserRow{}
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.label), query) {
			out = append(out, r)
		}
	}
	return out
}
func (w *workbenchModel) selected() *browserRow {
	rows := w.visible()
	i := w.state().cursor
	if i < 0 || i >= len(rows) {
		return nil
	}
	return &rows[i]
}
func (w *workbenchModel) setRows(rows []browserRow) {
	old := ""
	if r := w.selected(); r != nil {
		old = r.id
	}
	if w.target != "" {
		old = w.target
	}
	w.state().rows = rows
	shown := w.visible()
	w.state().cursor = min(w.state().cursor, max(0, len(shown)-1))
	found := false
	for i, r := range shown {
		if r.id == old {
			w.state().cursor = i
			found = true
			break
		}
	}
	if w.target != "" && !found {
		missing := browserRow{id: w.target, label: "Requested target was not resolved", detail: "The selected static-preview target is absent from Ansible's resolved inventory.\n\nTarget: " + w.target + "\n\nUse Tab and j/k to inspect the available hosts and groups."}
		w.state().rows = append([]browserRow{missing}, w.state().rows...)
		w.state().cursor = 0
	}
	w.target = ""
	w.refreshDetail()
}
func (w *workbenchModel) refreshDetail() {
	body := "No matching rows. / changes the filter."
	if row := w.selected(); row != nil {
		body = row.detail
	}
	w.viewport.SetContent(ansi.Hardwrap(body, max(1, w.viewport.Width), true))
	w.viewport.GotoTop()
}

type inspectionMsg struct {
	generation uint64
	topic      string
	rows       []browserRow
	source     string
	err        error
}
type runtimeObservedMsg struct {
	id      uint64
	status  ansible.RuntimeStatus
	update  ansible.UpdateStatus
	checked bool
	err     error
}
type runtimeChangedMsg struct {
	result ansible.Result
	err    error
}

func (a *App) projectContext() ansible.ProjectContext {
	dir := a.config.PlaybookDir
	if pb := a.pbPanel.SelectedPlaybook(); pb != nil {
		dir = filepath.Dir(pb.Path)
	}
	if dir == "" {
		dir = a.config.WorkDir
	}
	return ansible.ProjectContext{WorkDir: a.config.WorkDir, Inventory: a.config.InventoryPath, PlaybookDir: dir, Executable: a.config.Runtime.Executable}
}
func (a *App) openWorkbench(topic, target string) tea.Cmd {
	w := a.workbench
	w.projectOverride = nil
	if w.cancel != nil {
		w.cancel()
	}
	w.generation++
	w.topic = topic
	w.target = target
	w.err = ""
	w.filtering = false
	w.input.Blur()
	if target != "" {
		w.state().query = ""
		w.state().detail = true
	}
	w.input.SetValue(w.state().query)
	a.mode = AppModeWorkbench
	a.syncWorkbenchLayout()
	if topic == "runtime" {
		a.renderRuntimeRows()
		return a.runtimeRefreshCmd(false, false)
	}
	return a.inspectCmd()
}
func (a *App) inspectCmd() tea.Cmd {
	w := a.workbench
	w.generation++
	generation, topic := w.generation, w.topic
	project := a.projectContext()
	if w.projectOverride != nil {
		project = *w.projectOverride
	}
	configPath := a.config.ConfigPath
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	if w.cancel != nil {
		w.cancel()
	}
	w.cancel = cancel
	w.pending = true
	return func() tea.Msg {
		defer cancel()
		msg := inspectionMsg{generation: generation, topic: topic}
		switch topic {
		case "inventory":
			snapshot, err := ansible.InspectInventory(ctx, project)
			msg.err = err
			for _, g := range snapshot.Groups {
				msg.rows = append(msg.rows, browserRow{"group:" + g.Name, "group  " + g.Name, pretty(g), g.Name})
			}
			for _, h := range snapshot.Hosts {
				msg.rows = append(msg.rows, browserRow{"host:" + h.Name, "host   " + h.Name, "Inventory-resolved variables (not task runtime vars):\n\n" + pretty(h), h.Name})
			}
			msg.source = "ansible-inventory --list · " + snapshot.ObservedAt.Format(time.RFC3339) + "\n" + snapshot.Warnings
		case "config":
			snapshot, err := ansible.InspectConfig(ctx, project)
			msg.err = err
			for _, entry := range snapshot.Entries {
				msg.rows = append(msg.rows, browserRow{entry.Name, entry.Name, pretty(entry), ""})
			}
			msg.rows = append(msg.rows, browserRow{"lazyansible-overrides", "lazyansible execution overrides", "For TUI/CLI managed runs:\nANSIBLE_STDOUT_CALLBACK=default\nANSIBLE_NOCOLOR=1\n\nThe managed ansible.cfg file is unchanged.", ""})
			msg.source = "ansible-config · changed settings and origins · " + snapshot.ObservedAt.Format(time.RFC3339) + "\n" + snapshot.Warnings
		case "settings":
			cfg, err := appcfg.Load(configPath)
			msg.err = err
			msg.rows = []browserRow{{"preferences", "File preferences", pretty(cfg), ""}, {"paths", "Storage and selected file", fmt.Sprintf("Config: %s\nProfiles: %s\nHistory: %s\nUpdate cache: %s\n\nE opens the selected config. Restart to apply preferences.", configPath, paths.ConfigDir(), paths.StateDir(), paths.CacheDir()), ""}, {"project", "Effective launch context", pretty(project), ""}}
			msg.source = "lazyansible preferences · " + configPath
		}
		msg.source = "cwd: " + project.WorkDir + "\ninventory: " + firstNonempty(project.Inventory, "Ansible default") + "\n" + msg.source
		return msg
	}
}
func (a *App) runtimeRefreshCmd(check, force bool) tea.Cmd {
	if a.runtimePending && !force {
		return nil
	}
	if a.runtimeCancel != nil {
		a.runtimeCancel()
	}
	a.runtimeID++
	id := a.runtimeID
	a.runtimePending = true
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	a.runtimeCancel = cancel
	opts := a.config.Runtime
	if opts.CacheDir == "" {
		opts.CacheDir = paths.CacheDir()
	}
	return func() tea.Msg {
		defer cancel()
		if check {
			update, err := ansible.Check(ctx, opts, force)
			return runtimeObservedMsg{id: id, status: update.Runtime, update: update, checked: true, err: err}
		}
		status, err := ansible.Status(ctx, opts)
		return runtimeObservedMsg{id: id, status: status, err: err}
	}
}
func (a *App) renderRuntimeRows() {
	w := a.workbench
	w.setRows([]browserRow{{"installed", "Active Ansible installation", pretty(w.runtime), ""}, {"updates", "Latest update observation", pretty(w.update) + "\n\nA newer release may be outside an installed version constraint.\nInstall and upgrade require review. Other tools are not upgraded.", ""}})
	w.source = "Shared uv tool · c check now · u review upgrade · i review install"
}
func (a *App) updateWorkbenchResult(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case inspectionMsg:
		w := a.workbench
		if m.generation != w.generation || m.topic != w.topic {
			return nil, true
		}
		w.pending = false
		if m.err != nil {
			w.err = m.err.Error()
		} else {
			w.err = ""
			w.source = m.source
			w.setRows(append(m.rows, browserRow{"observation", "Observation source / diagnostics", m.source, ""}))
		}
		a.syncWorkbenchLayout()
		return nil, true
	case runtimeObservedMsg:
		if m.id != a.runtimeID {
			return nil, true
		}
		previous := a.workbench.runtime
		if previous.Executable == "" && a.executionPreview.result != nil {
			previous = a.executionPreview.result.Runtime
		}
		if previous.Executable == "" && a.review.plan != nil {
			previous = a.review.plan.Runtime
		}
		if previous.Executable != "" && m.status.Executable != "" && runtimeIdentity(previous) != runtimeIdentity(m.status) {
			a.invalidateExecution("Observed Ansible runtime changed")
		}
		if a.workbench.update.Runtime.ToolVersion != "" && a.workbench.update.Runtime.ToolVersion != m.status.ToolVersion {
			a.workbench.update = ansible.UpdateStatus{State: "unknown", Runtime: m.status}
		}
		a.runtimePending = false
		a.workbench.runtime = m.status
		if m.checked {
			a.workbench.update = m.update
		}
		if m.err != nil {
			a.runtimeSummary = "Ansible update/status unknown"
			if a.workbench.topic == "runtime" {
				a.workbench.err = m.err.Error()
			}
		} else {
			a.runtimeSummary = "Ansible " + m.status.CoreVersion
			if m.checked && m.update.State == "available" {
				a.runtimeSummary += " · update " + m.update.LatestVersion
			}
			if a.workbench.topic == "runtime" {
				a.workbench.err = ""
			}
		}
		if a.workbench.topic == "runtime" {
			a.renderRuntimeRows()
		}
		return nil, true
	case runtimeChangedMsg:
		a.invalidateExecution("Ansible runtime changed")
		a.runtimeBusy = false
		if a.quitting {
			return a.quitWhenIdle(), true
		}
		if m.err != nil {
			a.statusMsg = "Runtime change failed: " + m.err.Error()
		} else if m.result.ExitCode != 0 {
			a.statusMsg = fmt.Sprintf("Runtime change exited %d — see logs", m.result.ExitCode)
		} else {
			a.statusMsg = "Runtime operation completed — verifying active version"
		}
		level := core.LogLevelInfo
		if m.err != nil || m.result.ExitCode != 0 {
			level = core.LogLevelFailed
		}
		a.logsPanel.AddLine(core.LogLine{Text: a.statusMsg, Level: level, Timestamp: time.Now()})
		return a.runtimeRefreshCmd(false, true), true
	case runPreparedMsg:
		return a.acceptRunPlan(m), true
	case historySavedMsg:
		if a.historyJobs > 0 {
			a.historyJobs--
		}
		if m.err != nil {
			a.statusMsg = "Run completed; history could not be saved: " + m.err.Error()
		}
		return a.quitWhenIdle(), true
	}
	return nil, false
}

func runtimeIdentity(r ansible.RuntimeStatus) string {
	return strings.Join([]string{r.Executable, r.PlaybookExecutable, r.CoreVersion, r.Python, r.PythonVersion, r.ToolVersion}, "\x00")
}

func (a *App) openRunHostInspector(target string) tea.Cmd {
	_ = a.openWorkbench("inventory", target)
	project := a.lastRunRequest.Project
	a.workbench.projectOverride = &project
	return a.inspectCmd()
}
func (a *App) updateWorkbench(msg tea.Msg) tea.Cmd {
	defer a.syncWorkbenchLayout()
	w := a.workbench
	s := w.state()
	if key, ok := msg.(tea.KeyMsg); ok {
		if w.filtering {
			switch key.String() {
			case "esc":
				w.filtering = false
				w.input.Blur()
				w.input.SetValue("")
				s.query = ""
				s.cursor = 0
				w.refreshDetail()
				return nil
			case "enter":
				w.filtering = false
				w.input.Blur()
				return nil
			case "up", "down":
				if key.String() == "up" {
					s.cursor = max(0, s.cursor-1)
				} else {
					s.cursor = min(max(0, len(w.visible())-1), s.cursor+1)
				}
				w.refreshDetail()
				return nil
			}
			before := w.input.Value()
			var cmd tea.Cmd
			w.input, cmd = w.input.Update(msg)
			if before != w.input.Value() {
				s.query = w.input.Value()
				s.cursor = 0
				w.refreshDetail()
			}
			return cmd
		}
		switch key.String() {
		case "esc", "q":
			if w.cancel != nil {
				w.cancel()
			}
			w.generation++
			w.pending = false
			a.mode = AppModeNormal
			return nil
		case "/":
			w.filtering = true
			w.input.SetValue(s.query)
			return w.input.Focus()
		case "tab", "shift+tab", "enter":
			s.detail = !s.detail
			return nil
		case "r":
			if w.topic == "runtime" {
				return a.runtimeRefreshCmd(false, false)
			}
			return a.inspectCmd()
		case "s":
			if w.topic == "inventory" && !w.pending && w.err == "" {
				if row := w.selected(); row != nil && row.target != "" {
					a.pbPanel.SetLimit(row.target)
					a.statusMsg = "Limit → " + row.target
				}
			}
			return nil
		case "c":
			if w.topic == "runtime" {
				return a.runtimeRefreshCmd(true, true)
			}
		case "u":
			if w.topic == "runtime" {
				return a.reviewRuntime("upgrade")
			}
		case "i":
			if w.topic == "runtime" {
				return a.reviewRuntime("install")
			}
		case "E":
			if w.topic == "settings" {
				path := a.config.ConfigPath
				return func() tea.Msg {
					if _, err := os.Stat(path); os.IsNotExist(err) {
						if err = appcfg.WriteExample(path); err != nil {
							return errMsg{err: err}
						}
					}
					return editorOpenPathMsg{path: path}
				}
			}
		}
		if !s.detail {
			switch key.String() {
			case "j", "down":
				s.cursor = min(max(0, len(w.visible())-1), s.cursor+1)
			case "k", "up":
				s.cursor = max(0, s.cursor-1)
			case "g", "home":
				s.cursor = 0
			case "G", "end":
				s.cursor = max(0, len(w.visible())-1)
			case "h", "left":
				s.detail = false
			case "l", "right":
				s.detail = true
			}
			w.refreshDetail()
			return nil
		}
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "g", "home":
			w.viewport.GotoTop()
			return nil
		case "G", "end":
			w.viewport.GotoBottom()
			return nil
		case "h", "left":
			s.detail = false
			return nil
		}
	}
	var cmd tea.Cmd
	w.viewport, cmd = w.viewport.Update(msg)
	return cmd
}
func (a *App) workbenchHeader() string {
	w := a.workbench
	s := w.state()
	title := "Inspector / " + w.topic
	if w.topic == "runtime" || w.topic == "settings" {
		title = "lazyansible / " + w.topic
	}
	if w.pending || a.runtimePending && w.topic == "runtime" {
		title += " · loading"
	}
	if a.runtimeBusy {
		title += " · runtime operation in progress"
	}
	source := strings.Split(w.source, "\n")
	header := title
	for _, line := range source[:min(2, len(source))] {
		header += "\n" + line
	}
	if w.err != "" {
		header += "\nError (previous results retained): " + strings.ReplaceAll(w.err, "\n", " ")
	}
	if w.filtering {
		header += "\n" + w.input.View()
	} else if s.query != "" {
		header += "\nFilter: " + s.query
	}
	return fitScreen(header, max(1, a.width), 5)
}
func (a *App) workbenchDimensions() (int, int) {
	width := max(1, a.width)
	detailW := max(1, width-width/3-3)
	if width < 90 {
		detailW = width
	}
	return detailW, max(1, a.height-lipgloss.Height(a.workbenchHeader())-2)
}
func (a *App) syncWorkbenchLayout() {
	w := a.workbench
	if w == nil {
		return
	}
	width, height := a.workbenchDimensions()
	if w.viewport.Width != width || w.viewport.Height != height {
		offset := w.viewport.YOffset
		w.viewport.Width = width
		w.viewport.Height = height
		w.refreshDetail()
		w.viewport.SetYOffset(offset)
	}
}
func (a *App) workbenchView() string {
	w := a.workbench
	s := w.state()
	width, height := max(1, a.width), max(1, a.height)
	header := a.workbenchHeader()
	_, contentH := a.workbenchDimensions()
	rows := w.visible()
	start := max(0, s.cursor-contentH+1)
	end := min(len(rows), start+contentH)
	var list strings.Builder
	if len(rows) == 0 {
		list.WriteString("No results. r refreshes.")
	}
	listW := max(1, width/3)
	if width < 90 {
		listW = width
	}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == s.cursor {
			prefix = "> "
		}
		list.WriteString(ansi.Truncate(prefix+rows[i].label, listW, "…"))
		list.WriteByte('\n')
	}
	detail := w.viewport.View()
	if width < 90 {
		if s.detail {
			detail = fitScreen(detail, width, contentH)
		} else {
			detail = fitScreen(list.String(), width, contentH)
		}
	} else {
		detail = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(listW).Render(list.String()), " │ ", detail)
	}
	footer := "j/k select · Tab detail · / filter · r refresh · Esc back"
	if w.topic == "inventory" {
		footer += " · s limit"
	}
	if w.topic == "runtime" {
		footer = "c check · u upgrade · i install · Tab detail · Esc back"
	}
	if w.topic == "settings" {
		footer += " · E edit"
	}
	return fitScreen(header+"\n"+detail+"\n"+footer, width, height)
}
func pretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
func firstNonempty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
func fitScreen(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}
