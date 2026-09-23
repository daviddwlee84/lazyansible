package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type paletteModel struct {
	input  textinput.Model
	cursor int
}

func newPalette() *paletteModel {
	input := textinput.New()
	input.Prompt = "Find action: "
	return &paletteModel{input: input}
}
func (p *paletteModel) reset() { p.input.SetValue(""); p.input.Focus(); p.cursor = 0 }
func (a *App) paletteActions() []action {
	var out []action
	query := strings.ToLower(a.palette.input.Value())
	for _, v := range a.actions() {
		if v.id == "palette" || v.kind == "panel" || strings.HasPrefix(v.id, "focus-") {
			continue
		}
		if strings.Contains(strings.ToLower(v.label), query) {
			out = append(out, v)
		}
	}
	return out
}
func (a *App) updatePalette(msg tea.Msg) tea.Cmd {
	p := a.palette
	rows := a.paletteActions()
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			a.mode = AppModeNormal
			return nil
		case "up":
			p.cursor = max(0, p.cursor-1)
			return nil
		case "down":
			p.cursor = min(max(0, len(rows)-1), p.cursor+1)
			return nil
		case "enter":
			if p.cursor < len(rows) && p.cursor >= 0 {
				v := rows[p.cursor]
				if !v.enabled {
					return nil
				}
				a.mode = AppModeNormal
				return a.dispatchAction(v)
			}
			return nil
		}
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.cursor = 0
	}
	return cmd
}
func (a *App) paletteView() string {
	p := a.palette
	rows := a.paletteActions()
	height := max(1, a.height-5)
	start := max(0, p.cursor-height+1)
	end := min(len(rows), start+height)
	var b strings.Builder
	b.WriteString("Actions — type to search\n" + p.input.View() + "\n\n")
	if len(rows) == 0 {
		b.WriteString("No matching actions.\n")
	}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == p.cursor {
			prefix = "> "
		}
		label := prefix + rows[i].label
		if !rows[i].enabled {
			label += " (unavailable)"
		}
		b.WriteString(ansi.Truncate(label, max(1, a.width), "…") + "\n")
	}
	b.WriteString("↑/↓ choose · Enter open · Esc back")
	return fitScreen(b.String(), max(1, a.width), max(1, a.height))
}
