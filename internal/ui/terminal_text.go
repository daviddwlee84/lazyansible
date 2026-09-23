package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/x/ansi"
)

// plainTerminalText is for external text before application styling. It removes
// terminal commands without interpreting source contents or changing stored data.
func plainTerminalText(value string) string {
	var text strings.Builder
	for _, character := range ansi.Strip(value) {
		switch character {
		case '\n':
			text.WriteRune(character)
		case '\t':
			text.WriteString("    ")
		default:
			if !unicode.IsControl(character) {
				text.WriteRune(character)
			}
		}
	}
	return text.String()
}

func plainTerminalLine(value string) string {
	return strings.ReplaceAll(plainTerminalText(value), "\n", " ")
}

// Sanitize only a display copy so the editor's actual value/cursor and its
// intentional cursor/style ANSI remain intact.
func terminalInputView(input textinput.Model) string {
	value := input.Value()
	safe := plainTerminalLine(value)
	if safe != value {
		runes := []rune(value)
		position := max(0, min(input.Position(), len(runes)))
		cursor := len([]rune(plainTerminalLine(string(runes[:position]))))
		input.SetValue(safe)
		input.SetCursor(cursor)
	}
	return input.View()
}
