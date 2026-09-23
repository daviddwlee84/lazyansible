package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/daviddwlee84/lazyansible/internal/core"
)

func TestHelpKeepsColumnsAlignedInsideCenteredOverlay(t *testing.T) {
	a := workbenchFixture(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.focused = core.PanelPlaybooks
	a.updateFocus()
	a.mode = AppModeHelp
	view := ansi.Strip(a.View())
	labels := []string{"Search / filter", "View playbook", "Review and run", "Toggle check mode", "Toggle diff mode", "Select tags", "Extra variables", "Edit selected source"}
	keyColumn, descriptionColumn := -1, -1
	for _, label := range labels {
		found := false
		for _, line := range strings.Split(view, "\n") {
			index := strings.Index(line, label)
			if index < 0 {
				continue
			}
			found = true
			keyStart := len(line) - len(strings.TrimLeft(line, " "))
			labelStart := lipgloss.Width(line[:index])
			if keyColumn < 0 {
				keyColumn = keyStart
				descriptionColumn = labelStart
			}
			if keyStart != keyColumn || labelStart != descriptionColumn {
				t.Fatalf("%q drifted: key column %d/%d, description %d/%d", label, keyStart, keyColumn, labelStart, descriptionColumn)
			}
			break
		}
		if !found {
			t.Fatalf("current action %q missing from first Help page", label)
		}
	}
	if !strings.Contains(view, "Enter / Space") {
		t.Fatal("Space alias must have a visible name")
	}
	width, _, _ := a.helpSize()
	for _, line := range strings.Split(a.actionsHelp(), "\n") {
		if lipgloss.Width(line) != width {
			t.Fatalf("Help line width %d != fixed block width %d", lipgloss.Width(line), width)
		}
	}
}

func TestHelpGroupsCurrentActionsAndUnavailableStates(t *testing.T) {
	a := workbenchFixture(t)
	a.focused = core.PanelPlaybooks
	a.updateFocus()
	a.runtimeBusy = true
	rows := ansi.Strip(strings.Join(a.helpRows(76), "\n"))
	current, navigation, global := strings.Index(rows, "Playbooks · current panel"), strings.Index(rows, "Navigation"), strings.Index(rows, "Global actions")
	if current < 0 || !(current < navigation && navigation < global) {
		t.Fatal("expected current panel, navigation, then global sections")
	}
	if !strings.Contains(rows, "Review and run (unavailable)") {
		t.Fatal("disabled run lost its explicit unavailable label")
	}
	if strings.Index(rows, "View playbook") > navigation || strings.Index(rows, "Role browser") < global {
		t.Fatal("actions rendered under the wrong section")
	}
	if strings.Contains(rows, "Set target limit") {
		t.Fatal("Help advertised an action from a different panel")
	}
	a.focused = core.PanelInventory
	a.updateFocus()
	rows = ansi.Strip(strings.Join(a.helpRows(76), "\n"))
	if !strings.Contains(rows, "Inventory · current panel") || !strings.Contains(rows, "Set target limit") || strings.Contains(rows, "View playbook") {
		t.Fatal("Help did not follow current panel context")
	}
}

func TestHelpBoundsAcrossTerminalSizes(t *testing.T) {
	a := workbenchFixture(t)
	a.mode = AppModeHelp
	for _, size := range [][2]int{{160, 50}, {80, 24}, {44, 12}, {26, 8}, {8, 3}, {1, 1}, {0, 0}, {80, 24}} {
		a.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, panel := range []core.Panel{core.PanelInventory, core.PanelPlaybooks, core.PanelStatus, core.PanelLogs} {
			a.focused = panel
			a.updateFocus()
			view := a.actionsHelp()
			if size[0] == 0 || size[1] == 0 {
				if view != "" {
					t.Fatal("zero-size Help should be empty")
				}
				continue
			}
			if !utf8.ValidString(view) {
				t.Fatal("Help returned invalid UTF-8")
			}
			for _, rendered := range []string{view, a.View()} {
				if lipgloss.Height(rendered) > size[1] {
					t.Fatalf("Help exceeds terminal height %v", size)
				}
				for _, line := range strings.Split(rendered, "\n") {
					if lipgloss.Width(line) > size[0] {
						t.Fatalf("Help exceeds terminal width %v: %q", size, line)
					}
				}
			}
		}
	}
}

func TestHelpScrollReachesLastWrappedRowAndClamps(t *testing.T) {
	a := workbenchFixture(t)
	a.mode = AppModeHelp
	for _, size := range [][2]int{{80, 24}, {30, 10}, {120, 35}} {
		a.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		width, _, page := a.helpSize()
		rows := a.helpRows(width)
		lastOffset := max(0, len(rows)-page)
		a.Update(overlayKey("end"))
		if a.helpOffset != lastOffset {
			t.Fatalf("End offset %d != %d for %v", a.helpOffset, lastOffset, size)
		}
		lastRow := ansi.Strip(rows[len(rows)-1])
		if !strings.Contains(ansi.Strip(a.View()), lastRow) {
			t.Fatalf("final Help row unreachable at %v: %q", size, lastRow)
		}
		for i := 0; i < 20; i++ {
			a.Update(overlayKey("j"))
		}
		if a.helpOffset != lastOffset {
			t.Fatal("Help scrolled past the bottom")
		}
		a.Update(overlayKey("k"))
		if a.helpOffset != max(0, lastOffset-1) {
			t.Fatal("one Up press should leave the bottom immediately")
		}
		a.Update(overlayKey("home"))
		if a.helpOffset != 0 {
			t.Fatal("Home did not return to first page")
		}
	}
}

func TestHelpClosePreservesDashboardAndBindings(t *testing.T) {
	a := workbenchFixture(t)
	a.focused = core.PanelPlaybooks
	a.updateFocus()
	before := a.View()
	a.Update(overlayKey("?"))
	if a.mode != AppModeHelp {
		t.Fatal("? did not open Help")
	}
	a.Update(overlayKey("G"))
	a.Update(overlayKey("esc"))
	if a.mode != AppModeNormal || a.focused != core.PanelPlaybooks || a.View() != before {
		t.Fatal("closing Help changed dashboard context or rendering")
	}
	view, ok := a.actionForKey(" ")
	if !ok || view.id != "view" {
		t.Fatal("visible Space label changed its original binding")
	}
}
