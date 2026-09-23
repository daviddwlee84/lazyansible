package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazyansible/internal/core"
	invscan "github.com/daviddwlee84/lazyansible/internal/inventory"
	"github.com/daviddwlee84/lazyansible/internal/roles"
)

// RoleRunMsg is reserved for the explicit standalone-role action. Tags are
// intentionally empty unless that standalone request explicitly supplies them.
type RoleRunMsg struct{ RolePath, RoleName, Inventory, Limit, Tags string }

type roleRow struct {
	id, label   string
	role        *roles.Role
	declaration *core.RoleDeclaration
	explanation string
}
type roleNavigation struct {
	cursor, pane, offset int
	query                string
}

type rolesLoadedMsg struct {
	generation uint64
	context    string
	roles      []*roles.Role
	playbook   *core.Playbook
	err        error
}
type rolesSourceMsg struct {
	generation, request uint64
	path, content       string
	err                 error
}

// RolesOverlay retains its historical name while providing the Roles workspace
// body. All filesystem work is returned as commands, never done by Update/View.
type RolesOverlay struct {
	roles                      []*roles.Role
	related                    []roleRow
	playbook                   *core.Playbook
	cursor, pane, detailOff    int
	filter                     textinput.Model
	filtering                  bool
	projectMode                bool
	views                      [2]roleNavigation
	limit, inventory           string
	width, height              int
	err                        error
	loading                    bool
	generation                 uint64
	context                    string
	cancel                     context.CancelFunc
	sourceFiles                []roles.SourceFile
	sourceCursor, sourceOffset int
	sourceMode                 int // 0 role overview, 1 source list, 2 source preview
	sourceRequest              uint64
	sourcePending              bool
	sourceContent, sourceError string
	sourceCancel               context.CancelFunc
	parentContext              context.Context
	inputPlaybook              *core.Playbook
	dirty                      bool
	sourceTitle                string
}

func newRolesOverlay(width, height int) *RolesOverlay {
	input := textinput.New()
	input.Placeholder = "filter declarations…"
	input.Width = 20
	return &RolesOverlay{width: width, height: height, filter: input, parentContext: context.Background()}
}

func roleContext(rolesDir string, pb *core.Playbook) string {
	path := ""
	if pb != nil {
		path = pb.Path
	}
	return rolesDir + "\x00" + path
}
func cloneRolePlaybook(pb *core.Playbook) *core.Playbook {
	if pb == nil {
		return nil
	}
	copy := *pb
	copy.Hosts = append([]string{}, pb.Hosts...)
	copy.Tags = append([]string{}, pb.Tags...)
	copy.RoleDeclarations = append([]core.RoleDeclaration{}, pb.RoleDeclarations...)
	for i := range copy.RoleDeclarations {
		copy.RoleDeclarations[i].Tags = append([]string{}, copy.RoleDeclarations[i].Tags...)
	}
	return &copy
}

func (o *RolesOverlay) Open(ctx context.Context, rolesDir string, pb *core.Playbook, inventory, limit string) tea.Cmd {
	if o.cancel != nil {
		o.cancel()
	}
	key := roleContext(rolesDir, pb)
	if o.context != key {
		o.context = key
		o.roles = nil
		o.related = nil
		o.projectMode = false
		o.views = [2]roleNavigation{}
		o.cursor = 0
		o.pane = 0
		o.detailOff = 0
		o.filter.SetValue("")
		o.filter.Blur()
		o.filtering = false
		o.leaveSources()
	}
	o.inputPlaybook = cloneRolePlaybook(pb)
	o.playbook = cloneRolePlaybook(pb)
	if len(o.related) == 0 {
		o.related = relatedRoleRows(o.playbook, o.roles)
	}
	reloadSource := o.dirty && o.sourceMode == 2
	o.dirty = false
	o.inventory = inventory
	o.limit = limit
	o.err = nil
	o.loading = true
	o.parentContext = ctx
	o.generation++
	generation := o.generation
	child, cancel := context.WithCancel(ctx)
	o.cancel = cancel
	snapshot := cloneRolePlaybook(pb)
	scan := func() tea.Msg {
		defer cancel()
		found, err := roles.ScanContext(child, rolesDir)
		if err == nil && snapshot != nil {
			if current, ok := invscan.ParseSinglePlaybook(snapshot.Path); ok {
				snapshot = current
			} else {
				err = fmt.Errorf("cannot parse selected playbook: %s", snapshot.Path)
			}
		}
		return rolesLoadedMsg{generation: generation, context: key, roles: found, playbook: snapshot, err: err}
	}
	if reloadSource {
		return tea.Batch(scan, o.loadSource())
	}
	return scan
}

// Load is a compatibility entry point. Callers must return the command.
func (o *RolesOverlay) Load(rolesDir, inventory, limit string) tea.Cmd {
	return o.Open(context.Background(), rolesDir, nil, inventory, limit)
}
func (o *RolesOverlay) Apply(msg tea.Msg) bool {
	switch m := msg.(type) {
	case rolesLoadedMsg:
		if m.generation != o.generation || m.context != o.context {
			return true
		}
		old := ""
		if row := o.selectedRow(); row != nil {
			old = row.id
		}
		o.loading = false
		o.err = m.err
		if m.err == nil {
			o.roles = m.roles
			o.playbook = m.playbook
			o.related = relatedRoleRows(o.playbook, m.roles)
			o.restoreIdentity(old)
		}
		return true
	case rolesSourceMsg:
		// A metadata refresh may finish while this same source is loading.
		// Context changes/back navigation increment sourceRequest separately.
		if m.request != o.sourceRequest || o.sourceMode != 2 || o.sourceCursor >= len(o.sourceFiles) || o.sourceFiles[o.sourceCursor].Path != m.path {
			return true
		}
		o.sourcePending = false
		o.sourceContent = m.content
		o.sourceError = ""
		if m.err != nil {
			o.sourceError = m.err.Error()
		}
		return true
	}
	return false
}
func relatedRoleRows(pb *core.Playbook, project []*roles.Role) []roleRow {
	if pb == nil {
		return nil
	}
	rows := make([]roleRow, 0, len(pb.RoleDeclarations))
	for _, decl := range pb.RoleDeclarations {
		copy := decl
		copy.Tags = append([]string{}, decl.Tags...)
		row := roleRow{id: decl.ID, declaration: &copy, label: decl.Name, explanation: decl.Reason}
		if decl.Static {
			for _, role := range project {
				if decl.Name == role.Name || (filepath.IsAbs(decl.Name) && filepath.Clean(decl.Name) == filepath.Clean(role.Path)) {
					row.role = role
					break
				}
			}
			if row.role == nil {
				row.explanation = "Literal declaration; no matching project role source. Collections and other roles_path locations are not scanned."
			}
		} else {
			row.label = decl.Kind + ": " + decl.Name
		}
		if decl.PlayIndex > 0 {
			row.label = fmt.Sprintf("%s · play %d", row.label, decl.PlayIndex)
		}
		rows = append(rows, row)
	}
	return rows
}
func (o *RolesOverlay) rows() []roleRow {
	if !o.projectMode {
		return o.related
	}
	rows := make([]roleRow, 0, len(o.roles))
	for _, role := range o.roles {
		rows = append(rows, roleRow{id: role.Path, label: role.Name, role: role, explanation: "Project role source. Switch to related declarations to inspect observed playbook use."})
	}
	return rows
}
func (o *RolesOverlay) visibleRows() []roleRow {
	all := o.rows()
	query := strings.ToLower(o.filter.Value())
	if query == "" {
		return all
	}
	out := make([]roleRow, 0, len(all))
	for _, row := range all {
		if strings.Contains(strings.ToLower(row.label), query) {
			out = append(out, row)
		}
	}
	return out
}
func (o *RolesOverlay) selectedRow() *roleRow {
	rows := o.visibleRows()
	if o.cursor < 0 || o.cursor >= len(rows) {
		return nil
	}
	return &rows[o.cursor]
}
func (o *RolesOverlay) selected() *roles.Role {
	if row := o.selectedRow(); row != nil {
		return row.role
	}
	return nil
}
func (o *RolesOverlay) restoreIdentity(id string) {
	rows := o.visibleRows()
	o.cursor = max(0, min(o.cursor, len(rows)-1))
	for i, row := range rows {
		if row.id == id {
			o.cursor = i
			return
		}
	}
}
func (o *RolesOverlay) DeclaredTags() []string {
	if row := o.selectedRow(); row != nil && row.declaration != nil && row.declaration.Static {
		return append([]string{}, row.declaration.Tags...)
	}
	return nil
}
func (o *RolesOverlay) StandaloneRequest() (RoleRunMsg, bool) {
	role := o.selected()
	if role == nil || role.Path == "" || o.loading || o.err != nil {
		return RoleRunMsg{}, false
	}
	return RoleRunMsg{RolePath: role.Path, RoleName: role.Name, Inventory: o.inventory, Limit: o.limit, Tags: ""}, true
}
func (o *RolesOverlay) toggleProject() {
	index := 0
	if o.projectMode {
		index = 1
	}
	o.views[index] = roleNavigation{o.cursor, o.pane, o.detailOff, o.filter.Value()}
	o.projectMode = !o.projectMode
	index = 1 - index
	state := o.views[index]
	o.cursor = state.cursor
	o.pane = state.pane
	o.detailOff = state.offset
	o.filter.SetValue(state.query)
	o.filtering = false
	o.filter.Blur()
	o.restoreIdentity("")
}
func (o *RolesOverlay) leaveSources() {
	if o.sourceCancel != nil {
		o.sourceCancel()
	}
	o.sourceRequest++
	o.sourceMode = 0
	o.sourcePending = false
	o.sourceError = ""
	o.sourceContent = ""
}
func (o *RolesOverlay) HandleEscape() bool {
	if o.filtering {
		o.filtering = false
		o.filter.Blur()
		return true
	}
	if o.sourceMode == 2 {
		if o.sourceCancel != nil {
			o.sourceCancel()
		}
		o.sourceRequest++
		o.sourcePending = false
		o.sourceMode = 1
		return true
	}
	if o.sourceMode == 1 {
		o.leaveSources()
		return true
	}
	if o.pane == 1 {
		o.pane = 0
		return true
	}
	if o.filter.Value() != "" {
		o.filter.SetValue("")
		o.restoreIdentity("")
		return true
	}
	return false
}
func (o *RolesOverlay) openSources() {
	row := o.selectedRow()
	if row == nil {
		return
	}
	files := []roles.SourceFile{}
	if row.declaration != nil && row.declaration.SourcePath != "" {
		files = append(files, roles.SourceFile{Kind: "declaration", Path: row.declaration.SourcePath, Line: row.declaration.Line})
	}
	if row.role != nil {
		files = append(files, row.role.Sources...)
	}
	o.sourceTitle = row.label
	o.sourceFiles = files
	o.sourceCursor = 0
	o.sourceOffset = 0
	o.sourceMode = 1
}
func (o *RolesOverlay) loadSource() tea.Cmd {
	if o.sourceCursor < 0 || o.sourceCursor >= len(o.sourceFiles) {
		return nil
	}
	source := o.sourceFiles[o.sourceCursor]
	o.sourceMode = 2
	o.sourcePending = true
	o.sourceError = ""
	o.sourceContent = ""
	o.sourceOffset = max(0, source.Line-3)
	if o.sourceCancel != nil {
		o.sourceCancel()
	}
	o.sourceRequest++
	request, generation := o.sourceRequest, o.generation
	ctx, cancel := context.WithCancel(o.parentContext)
	o.sourceCancel = cancel
	return func() tea.Msg {
		defer cancel()
		content, err := roles.ReadSource(ctx, source.Path)
		return rolesSourceMsg{generation: generation, request: request, path: source.Path, content: content, err: err}
	}
}
func (o *RolesOverlay) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if o.filtering {
		if ok {
			switch key.String() {
			case "enter", "esc", "tab", "ctrl+c":
				o.filtering = false
				o.filter.Blur()
				return nil
			case "up":
				o.cursor = max(0, o.cursor-1)
				return nil
			case "down":
				o.cursor = min(o.cursor+1, max(0, len(o.visibleRows())-1))
				return nil
			}
		}
		previous := o.filter.Value()
		var cmd tea.Cmd
		o.filter, cmd = o.filter.Update(msg)
		if previous != o.filter.Value() {
			o.cursor = 0
			o.detailOff = 0
		}
		return cmd
	}
	if !ok {
		return nil
	}
	if o.sourceMode != 0 {
		return o.updateSources(key)
	}
	switch key.String() {
	case "/":
		o.filtering = true
		return o.filter.Focus()
	case "esc":
		o.HandleEscape()
	case "h", "left":
		o.pane = 0
	case "l", "right", "enter":
		if o.selectedRow() != nil {
			o.pane = 1
		}
	case "a":
		o.toggleProject()
	case "f":
		o.openSources()
	default:
		count := len(o.visibleRows())
		offset := &o.cursor
		if o.pane == 1 {
			offset = &o.detailOff
			count = len(o.wrappedDetails(o.width))
		}
		switch key.String() {
		case "j", "down":
			*offset = min(*offset+1, max(0, count-1))
		case "k", "up":
			*offset = max(0, *offset-1)
		case "g", "home":
			*offset = 0
		case "G", "end":
			*offset = max(0, count-1)
		}
		if o.pane == 0 {
			o.detailOff = 0
		}
	}
	return nil
}
func (o *RolesOverlay) updateSources(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc", "h", "left":
		o.HandleEscape()
		return nil
	case "enter", "l", "right":
		if o.sourceMode == 1 {
			return o.loadSource()
		}
	case "j", "down":
		if o.sourceMode == 1 {
			o.sourceCursor = min(o.sourceCursor+1, max(0, len(o.sourceFiles)-1))
		} else {
			o.sourceOffset = min(o.sourceOffset+1, max(0, len(strings.Split(o.sourceContent, "\n"))-1))
		}
	case "k", "up":
		if o.sourceMode == 1 {
			o.sourceCursor = max(0, o.sourceCursor-1)
		} else {
			o.sourceOffset = max(0, o.sourceOffset-1)
		}
	case "g", "home":
		if o.sourceMode == 1 {
			o.sourceCursor = 0
		} else {
			o.sourceOffset = 0
		}
	case "G", "end":
		if o.sourceMode == 1 {
			o.sourceCursor = max(0, len(o.sourceFiles)-1)
		} else {
			o.sourceOffset = max(0, len(strings.Split(o.sourceContent, "\n"))-1)
		}
	}
	return nil
}
func (o *RolesOverlay) detailLines() []string {
	row := o.selectedRow()
	if row == nil {
		return []string{"Select a declaration to inspect."}
	}
	lines := []string{row.label}
	if ref := row.declaration; ref != nil {
		play := fmt.Sprintf("Play %d", ref.PlayIndex)
		if ref.PlayIndex == 0 {
			play = "Top-level import"
		}
		if ref.PlayName != "" {
			play += " · " + ref.PlayName
		}
		lines = append(lines, play, fmt.Sprintf("Declared: %s:%d", ref.SourcePath, ref.Line))
		if len(ref.Tags) > 0 {
			lines = append(lines, "Declared tags: "+strings.Join(ref.Tags, ", "), "Tags may select other tasks too; they are not a role-only scope.")
		}
		if ref.Static {
			lines = append(lines, "Declared in this file; execution conditions are not evaluated.")
		}
	}
	if row.explanation != "" {
		lines = append(lines, row.explanation)
	}
	role := row.role
	if role == nil {
		return append(lines, "", "f opens the declaration source; imports/includes are not expanded.")
	}
	lines = append(lines, "Local source: "+role.Path)
	if role.Desc != "" {
		lines = append(lines, role.Desc)
	}
	lines = append(lines, "", fmt.Sprintf("Tasks (%d)", len(role.Tasks)))
	for _, task := range role.Tasks {
		line := "  " + task.Name
		if task.Module != "" {
			line += " [" + task.Module + "]"
		}
		lines = append(lines, line)
	}
	if len(role.Defaults) > 0 {
		lines = append(lines, "", "Defaults")
		keys := make([]string, 0, len(role.Defaults))
		for key := range role.Defaults {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, "  "+key+": "+role.Defaults[key])
		}
	}
	if len(role.Handlers) > 0 {
		lines = append(lines, "", "Handlers")
		for _, name := range role.Handlers {
			lines = append(lines, "  "+name)
		}
	}
	if len(role.Deps) > 0 {
		lines = append(lines, "", "Dependencies")
		for _, name := range role.Deps {
			lines = append(lines, "  "+name)
		}
	}
	return append(lines, "", "r reviews the current playbook. Standalone role execution is an explicit Actions entry.")
}
func roleWindow(lines []string, offset, height int) string {
	if height <= 0 {
		return ""
	}
	offset = max(0, min(offset, max(0, len(lines)-height)))
	return strings.Join(lines[offset:min(len(lines), offset+height)], "\n")
}
func roleClip(text string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}
func (o *RolesOverlay) ContentView(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	title := "Related declarations"
	toggle := "a project roles"
	if o.projectMode {
		title = "Project roles"
		toggle = "a related declarations"
	}
	if o.playbook != nil {
		title += " · " + plainTerminalLine(o.playbook.Name)
	}
	if o.loading {
		title += " · loading…"
	}
	if o.sourceMode != 0 {
		return o.sourcesView(width, height)
	}
	header := overlayTitleStyle.Render(title) + "\n" + overlayMutedStyle.Render(toggle+" · Enter inspect · f sources · r review playbook · t tags")
	if o.filtering || o.filter.Value() != "" {
		filter := o.filter
		filter.Width = max(1, width-9)
		header += "\nFilter: " + terminalInputView(filter)
	}
	available := max(1, height-lipgloss.Height(header)-1)
	rows := o.visibleRows()
	list := []string{}
	if len(rows) == 0 {
		message := "No direct declarations found. Imports/includes may reference roles."
		if o.playbook == nil && !o.projectMode {
			message = "Select a playbook, or press a to browse project roles."
		}
		if o.projectMode {
			message = "No project roles found in roles/."
		}
		if o.filter.Value() != "" {
			message = "No matching declarations. / changes the filter."
		}
		if o.loading {
			message = "Loading role declarations and local sources…"
		}
		list = append(list, message)
	} else {
		for i, row := range rows {
			prefix := "  "
			if i == o.cursor {
				prefix = "› "
			}
			line := prefix + plainTerminalLine(row.label)
			if i == o.cursor {
				if o.pane == 0 {
					line = overlaySelectedStyle.Render(line)
				} else {
					line = overlayLabelStyle.Render(line)
				}
			}
			list = append(list, line)
		}
	}
	if o.err != nil {
		list = append([]string{"Discovery failed: " + plainTerminalLine(o.err.Error())}, list...)
	}
	listOffset := max(0, o.cursor-available+1)
	details := strings.Split(plainTerminalText(strings.Join(o.detailLines(), "\n")), "\n")
	var body string
	if width < 72 {
		if o.pane == 0 {
			body = roleWindow(list, listOffset, available)
		} else {
			body = roleWindow(strings.Split(ansi.Hardwrap(strings.Join(details, "\n"), width, true), "\n"), o.detailOff, available)
		}
	} else {
		left := max(22, min(36, width/3))
		right := max(1, width-left-3)
		leftBody := roleClip(roleWindow(list, listOffset, available), left, available)
		detailBody := roleWindow(strings.Split(ansi.Hardwrap(strings.Join(details, "\n"), right, true), "\n"), o.detailOff, available)
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(left).Render(leftBody), " │ ", lipgloss.NewStyle().Width(right).Render(detailBody))
	}
	return roleClip(header+"\n"+body, width, height)
}
func (o *RolesOverlay) sourcesView(width, height int) string {
	title := "Role sources"
	if o.sourceTitle != "" {
		title += " · " + plainTerminalLine(o.sourceTitle)
	}
	header := overlayTitleStyle.Render(title) + "\n" + overlayMutedStyle.Render("Enter/l opens · h/Esc back · j/k scroll")
	lines := []string{}
	offset := 0
	if o.sourceMode == 1 {
		for i, file := range o.sourceFiles {
			label := plainTerminalLine(file.Kind) + " · " + plainTerminalLine(file.Path)
			if file.Line > 0 {
				label += fmt.Sprintf(":%d", file.Line)
			}
			if i == o.sourceCursor {
				label = overlaySelectedStyle.Render("› " + label)
			} else {
				label = "  " + label
			}
			lines = append(lines, label)
		}
		if len(lines) == 0 {
			lines = []string{"No source main files found."}
		}
		offset = max(0, o.sourceCursor-max(1, height-3)+1)
	} else {
		if o.sourceCursor < len(o.sourceFiles) {
			header += "\n" + plainTerminalLine(o.sourceFiles[o.sourceCursor].Path)
		}
		if o.sourcePending {
			lines = []string{"Loading source…"}
		} else if o.sourceError != "" {
			lines = []string{"Cannot read source: " + plainTerminalLine(o.sourceError)}
		} else {
			for i, line := range strings.Split(plainTerminalText(o.sourceContent), "\n") {
				lines = append(lines, fmt.Sprintf("%4d  %s", i+1, line))
			}
		}
		offset = o.sourceOffset
	}
	return roleClip(header+"\n"+roleWindow(lines, offset, max(1, height-lipgloss.Height(header)-1)), width, height)
}
func (o *RolesOverlay) View() string { return o.ContentView(o.width, o.height) }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (o *RolesOverlay) wrappedDetails(width int) []string {
	if width >= 72 {
		width = max(1, width-max(22, min(36, width/3))-3)
	}
	return strings.Split(ansi.Hardwrap(plainTerminalText(strings.Join(o.detailLines(), "\n")), max(1, width), true), "\n")
}

// Invalidate preserves navigation while rejecting effects based on older source.
func (o *RolesOverlay) Invalidate() {
	o.dirty = true
	o.generation++
	o.loading = false
	if o.cancel != nil {
		o.cancel()
	}
	if o.sourceCancel != nil {
		o.sourceCancel()
	}
	o.sourceRequest++
	o.sourcePending = false
}
