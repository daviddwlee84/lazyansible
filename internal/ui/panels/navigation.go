package panels

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// listFilter owns printable keys while editing; navigation remains available
// with the arrow keys. Enter keeps the query and returns to list navigation.
type listFilter struct {
	input  textinput.Model
	active bool
}

func newListFilter() listFilter {
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "filter…"
	return listFilter{input: input}
}

func (f *listFilter) open() tea.Cmd {
	f.active = true
	return f.input.Focus()
}

func (f *listFilter) update(msg tea.Msg) (bool, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter", "esc", "ctrl+c", "tab":
			f.active = false
			f.input.Blur()
			return false, nil
		}
	}
	previous := f.input.Value()
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return previous != f.input.Value(), cmd
}

func (f *listFilter) query() string { return strings.ToLower(f.input.Value()) }

func (f *listFilter) view(width int) string {
	if !f.active && f.input.Value() == "" {
		return ""
	}
	f.input.Width = max(1, width-3)
	return cellTruncate(f.input.View(), width) + "\n"
}

func cellTruncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(text, width, "…")
}

func clipWidth(text string, width int) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = cellTruncate(line, width)
	}
	return strings.Join(lines, "\n")
}

func clampCursor(cursor, length int) int {
	return max(0, min(cursor, length-1))
}
