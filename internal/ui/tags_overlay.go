package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TagsConfirmedMsg is sent when the user confirms tag selection.
type TagsConfirmedMsg struct {
	Tags         string
	ID, Revision uint64
	Target       string
}

// TagsOverlay shows the available tags for a playbook with multi-select.
type TagsOverlay struct {
	allTags               []string // full list
	visible               []string // filtered list
	selected              map[string]bool
	cursor                int
	filter                textinput.Model
	filtering             bool
	width                 int
	height                int
	id, revision          uint64
	target, title, notice string
	pending               bool
	sources               map[string]string
}

func newTagsOverlay(width, height int) *TagsOverlay {
	ti := textinput.New()
	ti.Placeholder = "filter tags…"
	ti.Width = 30

	return &TagsOverlay{
		selected: make(map[string]bool),
		filter:   ti,
		width:    width,
		height:   height,
	}
}

func (t *TagsOverlay) SetTags(tags []string) {
	t.allTags = tags
	t.sources = make(map[string]string)
	for _, tag := range tags {
		t.sources[tag] = "local YAML"
	}
	t.selected = make(map[string]bool)
	t.filter.SetValue("")
	t.filtering = false
	t.filter.Blur()
	t.cursor = 0
	t.applyFilter()
}

func (t *TagsOverlay) applyFilter() {
	q := strings.ToLower(t.filter.Value())
	t.visible = t.visible[:0]
	for _, tag := range t.allTags {
		if q == "" || strings.Contains(strings.ToLower(tag), q) {
			t.visible = append(t.visible, tag)
		}
	}
	t.cursor = max(0, min(t.cursor, len(t.visible)-1))
}

// HandleEscape closes the nearest filter interaction before the overlay.
func (t *TagsOverlay) HandleEscape() bool {
	if t.filtering {
		t.filtering = false
		t.filter.Blur()
		return true
	}
	if t.filter.Value() != "" {
		t.filter.SetValue("")
		t.applyFilter()
		return true
	}
	return false
}

func (t *TagsOverlay) SetSelectedTags(tags string) {
	t.selected = make(map[string]bool)
	for _, tag := range strings.Split(tags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			t.selected[tag] = true
		}
	}
	var missing []string
	for tag := range t.selected {
		if _, ok := t.sources[tag]; !ok {
			missing = append(missing, tag)
		}
	}
	sort.Strings(missing)
	t.MergeTags(missing, "selected; not observed")
}

func (t *TagsOverlay) MergeTags(tags []string, source string) {
	if t.sources == nil {
		t.sources = map[string]string{}
	}
	selected := ""
	if t.cursor >= 0 && t.cursor < len(t.visible) {
		selected = t.visible[t.cursor]
	}
	for _, tag := range tags {
		if _, ok := t.sources[tag]; !ok {
			t.allTags = append(t.allTags, tag)
		}
		t.sources[tag] = source
	}
	t.applyFilter()
	for i, tag := range t.visible {
		if tag == selected {
			t.cursor = i
			break
		}
	}
}

// SelectedTagsString returns a comma-separated list of selected tags.
func (t *TagsOverlay) SelectedTagsString() string {
	var out []string
	for _, tag := range t.allTags { // preserves order
		if t.selected[tag] {
			out = append(out, tag)
		}
	}
	return strings.Join(out, ",")
}

func (t *TagsOverlay) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if t.filtering {
		if ok {
			switch key.String() {
			case "enter", "esc", "tab", "ctrl+c":
				t.filtering = false
				t.filter.Blur()
				return nil
			case "down":
				t.cursor = min(t.cursor+1, max(0, len(t.visible)-1))
				return nil
			case "up":
				t.cursor = max(0, t.cursor-1)
				return nil
			}
		}
		var cmd tea.Cmd
		previous := t.filter.Value()
		t.filter, cmd = t.filter.Update(msg)
		if previous != t.filter.Value() {
			t.cursor = 0
			t.applyFilter()
		}
		return cmd
	}
	if !ok {
		return nil
	}

	switch key.String() {
	case "/":
		t.filtering = true
		return t.filter.Focus()
	case "esc":
		t.HandleEscape()
	case "g", "home":
		t.cursor = 0
	case "G", "end":
		t.cursor = max(0, len(t.visible)-1)
	case "enter":
		selected := t.SelectedTagsString()
		id, revision, target := t.id, t.revision, t.target
		return func() tea.Msg {
			return TagsConfirmedMsg{Tags: selected, ID: id, Revision: revision, Target: target}
		}
	case "j", "down":
		if t.cursor < len(t.visible)-1 {
			t.cursor++
		}
		return nil
	case "k", "up":
		if t.cursor > 0 {
			t.cursor--
		}
		return nil
	case " ":
		if t.cursor < len(t.visible) {
			tag := t.visible[t.cursor]
			t.selected[tag] = !t.selected[tag]
		}
		return nil
	case "a":
		// Select all visible.
		for _, tag := range t.visible {
			t.selected[tag] = true
		}
		return nil
	case "A":
		// Deselect all.
		t.selected = make(map[string]bool)
		return nil
	}
	return nil
}

func (t *TagsOverlay) View() string {
	boxW := max(1, min(t.width, 76))
	boxH := max(1, min(t.height, len(t.visible)+8))

	var sb strings.Builder
	sb.WriteString(overlayTitleStyle.Render(plainTerminalLine(firstNonempty(t.title, "Tags"))+" · draft") + "\n")

	// Filter input.
	sb.WriteString(overlayLabelStyle.Render("Filter: ") + terminalInputView(t.filter) + "\n")
	state := strings.Join(strings.Fields(plainTerminalLine(t.notice)), " ")
	if t.pending {
		state = "Discovering Ansible tags… (local choices available)"
	}
	sb.WriteString(ansi.Truncate(state, boxW, "…") + "\n")

	if len(t.allTags) == 0 {
		sb.WriteString(overlayMutedStyle.Render("  No tags found in this playbook.") + "\n")
	} else if len(t.visible) == 0 {
		sb.WriteString(overlayMutedStyle.Render("  No tags match the filter.") + "\n")
	} else {
		contentH := boxH - 7
		if contentH < 1 {
			contentH = 1
		}
		start := 0
		if t.cursor >= contentH {
			start = t.cursor - contentH + 1
		}
		end := start + contentH
		if end > len(t.visible) {
			end = len(t.visible)
		}

		for i := start; i < end; i++ {
			tag := t.visible[i]
			check := "  "
			if t.selected[tag] {
				check = lipgloss.NewStyle().Foreground(lipgloss.Color("#22C55E")).Render("✓ ")
			}
			line := fmt.Sprintf("%s%s", check, plainTerminalLine(tag))
			if t.sources[tag] == "selected; not observed" {
				line += " (not observed)"
			}
			line = ansi.Truncate(line, boxW, "…")
			if i == t.cursor {
				sb.WriteString(overlaySelectedStyle.Render(line) + "\n")
			} else {
				sb.WriteString(overlayItemStyle.Render(line) + "\n")
			}
		}
	}

	// Show current selection.
	sel := t.SelectedTagsString()
	sb.WriteString("\n" + overlayLabelStyle.Render("Selected: ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#22C55E")).Render(plainTerminalLine(firstNonempty(sel, "No tag filter"))) + "\n")

	hint := "/ filter · Space toggle · a/A all/none\nEnter apply · Esc cancel"
	if t.filtering {
		hint = "Type to filter  [↑/↓] select  [enter/esc] return to list"
	}
	sb.WriteString(overlayLabelStyle.Render(hint))

	return fitScreen(sb.String(), boxW, boxH)
}
