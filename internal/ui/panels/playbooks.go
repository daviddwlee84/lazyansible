package panels

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/daviddwlee84/lazyansible/internal/core"
)

// RunRequestMsg asks the root model to review a run; it never starts a process.
type RunRequestMsg struct {
	Playbook *core.Playbook
	Limit    string
	Check    bool
	Diff     bool
	Tags     string
}

type ViewPlaybookMsg struct{ Playbook *core.Playbook }

// ExecutionOptions is owned by the application and shared with the panel for display.
// Standalone panel users receive a private instance until BindOptions is called.
type ExecutionOptions struct {
	Check, Diff            bool
	Tags, Limit, ExtraVars string
}
type ToggleOptionMsg struct{ Name string }

// PlaybooksPanel lists discovered playbooks and displays the application draft.
type PlaybooksPanel struct {
	playbooks    []*core.Playbook
	allPlaybooks []*core.Playbook
	cursor       int
	focused      bool
	width        int
	height       int
	filter       listFilter

	options *ExecutionOptions
}

func NewPlaybooksPanel(playbooks []*core.Playbook, width, height int) *PlaybooksPanel {
	return &PlaybooksPanel{playbooks: playbooks, allPlaybooks: playbooks, width: width, height: height, filter: newListFilter(), options: &ExecutionOptions{}}
}

func (p *PlaybooksPanel) BindOptions(options *ExecutionOptions) { p.options = options }

func (p *PlaybooksPanel) SetSize(w, h int)   { p.width = w; p.height = h }
func (p *PlaybooksPanel) SetFocused(f bool)  { p.focused = f }
func (p *PlaybooksPanel) FilterActive() bool { return p.filter.active }
func (p *PlaybooksPanel) SetPlaybooks(pbs []*core.Playbook) {
	selected := p.SelectedPlaybook()
	p.allPlaybooks = pbs
	p.applyFilter()
	if selected != nil {
		for i, pb := range p.playbooks {
			if pb.Path == selected.Path {
				p.cursor = i
				break
			}
		}
	}
}

func (p *PlaybooksPanel) applyFilter() {
	p.playbooks = nil
	q := p.filter.query()
	for _, pb := range p.allPlaybooks {
		if pb != nil && (q == "" || strings.Contains(strings.ToLower(pb.Name+" "+pb.Path), q)) {
			p.playbooks = append(p.playbooks, pb)
		}
	}
	p.cursor = clampCursor(p.cursor, len(p.playbooks))
}
func (p *PlaybooksPanel) SetLimit(limit string)     { p.options.Limit = limit }
func (p *PlaybooksPanel) SetActiveTags(tags string) { p.options.Tags = tags }
func (p *PlaybooksPanel) SetExtraVars(raw string)   { p.options.ExtraVars = raw }
func (p *PlaybooksPanel) SetCheckMode(v bool)       { p.options.Check = v }
func (p *PlaybooksPanel) SetDiffMode(v bool)        { p.options.Diff = v }
func (p *PlaybooksPanel) CurrentLimit() string      { return p.options.Limit }
func (p *PlaybooksPanel) CheckMode() bool           { return p.options.Check }
func (p *PlaybooksPanel) DiffMode() bool            { return p.options.Diff }

// SelectedTags returns the active tags as a slice (split by comma).
func (p *PlaybooksPanel) SelectedTags() []string {
	if p.options.Tags == "" {
		return nil
	}
	var tags []string
	for _, t := range strings.Split(p.options.Tags, ",") {
		if s := strings.TrimSpace(t); s != "" {
			tags = append(tags, s)
		}
	}
	return tags
}

// SelectByName moves the cursor to the playbook whose name matches.
func (p *PlaybooksPanel) SelectByName(name string) {
	p.SelectByNameUnique(name)
}

func (p *PlaybooksPanel) SelectByNameUnique(name string) bool {
	var match *core.Playbook
	for _, pb := range p.allPlaybooks {
		if pb.Name == name {
			if match != nil {
				return false
			}
			match = pb
		}
	}
	return match != nil && p.SelectByPath(match.Path)
}

func (p *PlaybooksPanel) SelectByPath(path string) bool {
	for _, pb := range p.allPlaybooks {
		if pb.Path == path {
			p.filter.input.SetValue("")
			p.applyFilter()
			for i, visible := range p.playbooks {
				if visible.Path == path {
					p.cursor = i
					return true
				}
			}
		}
	}
	return false
}

func (p *PlaybooksPanel) SelectedPlaybook() *core.Playbook {
	if p.cursor >= 0 && p.cursor < len(p.playbooks) {
		return p.playbooks[p.cursor]
	}
	return nil
}

func (p *PlaybooksPanel) Update(msg tea.Msg) tea.Cmd {
	if !p.focused {
		return nil
	}
	if p.filter.active {
		if key, ok := msg.(tea.KeyMsg); ok && (key.String() == "up" || key.String() == "down") {
			if key.String() == "up" {
				p.cursor--
			} else {
				p.cursor++
			}
			p.cursor = clampCursor(p.cursor, len(p.playbooks))
			return nil
		}
		changed, cmd := p.filter.update(msg)
		if changed {
			p.cursor = 0
			p.applyFilter()
		}
		return cmd
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "/":
		return p.filter.open()
	case "esc":
		selected := p.SelectedPlaybook()
		p.filter.input.SetValue("")
		p.applyFilter()
		if selected != nil {
			p.SelectByPath(selected.Path)
		}
	case "j", "down":
		if p.cursor < len(p.playbooks)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "g", "home":
		p.cursor = 0
	case "G", "end":
		if len(p.playbooks) > 0 {
			p.cursor = len(p.playbooks) - 1
		}
	case "c":
		return func() tea.Msg { return ToggleOptionMsg{Name: "check"} }
	case "d":
		return func() tea.Msg { return ToggleOptionMsg{Name: "diff"} }
	case "enter", " ":
		if pb := p.SelectedPlaybook(); pb != nil {
			return func() tea.Msg { return ViewPlaybookMsg{Playbook: pb} }
		}
	case "r":
		if pb := p.SelectedPlaybook(); pb != nil {
			request := RunRequestMsg{Playbook: pb, Limit: p.options.Limit, Check: p.options.Check, Diff: p.options.Diff, Tags: p.options.Tags}
			return func() tea.Msg {
				return request
			}
		}
	}
	return nil
}

func (p *PlaybooksPanel) View() string {
	filterView := p.filter.view(p.width)
	if len(p.playbooks) == 0 {
		if p.filter.query() != "" {
			return clipWidth(filterView+mutedText("No matching playbooks."), p.width)
		}
		return clipWidth(filterView+mutedText("No playbooks found.\nPlace *.yml files in your project directory."), p.width)
	}

	var sb strings.Builder
	sb.WriteString(filterView)

	// ── Active option badges ───────────────────────────────────────────────
	var badges []string
	if p.options.Check {
		badges = append(badges, flagStyle.Render("✓check"))
	}
	if p.options.Diff {
		badges = append(badges, flagStyle.Render("±diff"))
	}
	if p.options.Limit != "" {
		badges = append(badges, limitStyle.Render("⊢ "+truncateBadge(p.options.Limit, 14)))
	}
	if p.options.Tags != "" {
		badges = append(badges, tagsStyle.Render("# "+truncateBadge(p.options.Tags, 14)))
	}
	if p.options.ExtraVars != "" {
		badges = append(badges, extraVarsStyle.Render("-e values hidden"))
	}
	if len(badges) > 0 {
		sb.WriteString(strings.Join(badges, " ") + "\n")
	}

	// ── Playbook list ──────────────────────────────────────────────────────
	// Each non-selected entry = 1 line. Selected entry = 3 lines (name + hosts + tags/path).
	// Reserve space for the detail block of the selected item.
	detailLines := 2 // hosts row + tags/path row for selected item
	contentH := p.height - 4 - len(badges) - detailLines
	if filterView != "" {
		contentH--
	}
	if contentH < 1 {
		contentH = 1
	}
	start := 0
	if p.cursor >= contentH {
		start = p.cursor - contentH + 1
	}
	end := start + contentH
	if end > len(p.playbooks) {
		end = len(p.playbooks)
	}

	for i := start; i < end; i++ {
		pb := p.playbooks[i]
		selected := i == p.cursor && p.focused

		if selected {
			// ── Selected: name row ────────────────────────────────────────
			sb.WriteString(pbSelectedStyle.Render("▶ "+truncateBadge(pb.Name, p.width-4)) + "\n")

			// ── Hosts row ────────────────────────────────────────────────
			if len(pb.Hosts) > 0 {
				hostLabels := make([]string, 0, len(pb.Hosts))
				for _, h := range pb.Hosts {
					hostLabels = append(hostLabels, cleanHost(h))
				}
				hostsStr := strings.Join(hostLabels, ", ")
				hostsStr = truncateBadge(hostsStr, p.width-8)
				sb.WriteString(pbHostsStyle.Render("  hosts: "+hostsStr) + "\n")
			} else {
				sb.WriteString(pbHostsStyle.Render("  hosts: (not set)") + "\n")
			}

			// ── Tags / path row ───────────────────────────────────────────
			if len(pb.Tags) > 0 {
				tagStr := strings.Join(pb.Tags, ", ")
				tagStr = truncateBadge(tagStr, p.width-8)
				sb.WriteString(pbTagLineStyle.Render("  tags: "+tagStr) + "\n")
			} else {
				// Show short path hint when no tags.
				shortPath := truncateBadge(pb.Path, p.width-2)
				sb.WriteString(pbPathStyle.Render("  "+shortPath) + "\n")
			}
		} else {
			// ── Normal row: name + hosts summary on one line ──────────────
			hostsHint := ""
			if len(pb.Hosts) > 0 && p.width >= 30 {
				labels := make([]string, 0, len(pb.Hosts))
				for _, h := range pb.Hosts {
					labels = append(labels, cleanHost(h))
				}
				hostsHint = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#374151")).
					Render("  [" + truncateBadge(strings.Join(labels, ","), 16) + "]")
			}
			name := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9CA3AF")).
				Render("  " + truncateBadge(pb.Name, p.width-2-lipgloss.Width(hostsHint)))
			sb.WriteString(name + hostsHint + "\n")
		}
	}

	return clipWidth(sb.String(), p.width)
}

// cleanHost converts a raw Ansible hosts value into a human-friendly label.
// Jinja2 expressions like {{ target | default('all') }} become $target.
func cleanHost(h string) string {
	trimmed := strings.TrimSpace(h)
	if !strings.Contains(trimmed, "{{") {
		return trimmed
	}
	// Extract the variable name from {{ varname | ... }}
	inner := trimmed
	inner = strings.TrimPrefix(inner, "{{")
	inner = strings.TrimSuffix(inner, "}}")
	inner = strings.TrimSpace(inner)
	// Drop any filters (pipe onwards)
	if idx := strings.Index(inner, "|"); idx >= 0 {
		inner = inner[:idx]
	}
	varName := strings.TrimSpace(inner)
	if varName == "" {
		return "(dynamic)"
	}
	return "$" + varName
}

func truncateBadge(s string, max int) string {
	return cellTruncate(s, max)
}

var (
	pbSelectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#06B6D4")).
			Bold(true).
			Background(lipgloss.Color("#1F2937"))

	pbTagLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true)

	flagStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)

	limitStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22C55E")).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)

	tagsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#06B6D4")).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)

	extraVarsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A78BFA")).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)

	pbHostsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22C55E")).
			Italic(true)

	pbPathStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#374151")).
			Italic(true)
)
