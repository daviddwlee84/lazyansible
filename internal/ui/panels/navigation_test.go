package panels

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/daviddwlee84/lazyansible/internal/core"
)

func navKey(value string) tea.KeyMsg {
	keys := map[string]tea.KeyType{"enter": tea.KeyEnter, "esc": tea.KeyEsc, "up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd, "space": tea.KeySpace}
	if kind, ok := keys[value]; ok {
		if kind == tea.KeySpace {
			return tea.KeyMsg{Type: kind, Runes: []rune{' '}}
		}
		return tea.KeyMsg{Type: kind}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func fixtureInventory() *core.Inventory {
	return &core.Inventory{
		OrderedGroups: []string{"web", "db"},
		Groups:        map[string]*core.Group{"web": {Name: "web", Hosts: []string{"web-a", "web-b"}}, "db": {Name: "db", Hosts: []string{"db-a"}}},
		Hosts:         map[string]*core.Host{"web-a": {Name: "web-a"}, "web-b": {Name: "web-b"}, "db-a": {Name: "db-a"}},
	}
}

func TestInventoryInspectLimitTreeAndRefresh(t *testing.T) {
	p := NewInventoryPanel(fixtureInventory(), 24, 20)
	p.SetFocused(true)
	p.Update(navKey("l"))
	if p.SelectedHost() != "web-a" {
		t.Fatalf("expand/child selected %q", p.SelectedHost())
	}
	if got := p.Update(navKey("enter"))().(InspectInventoryMsg); got.Host != "web-a" {
		t.Fatalf("inspect = %+v", got)
	}
	if got := p.Update(navKey("s"))().(SetLimitMsg); got.Limit != "web-a" {
		t.Fatalf("limit = %+v", got)
	}
	p.Update(navKey("h"))
	p.Update(navKey("left"))
	if p.SelectedGroup() != "web" || !p.collapsed["web"] {
		t.Fatal("left should select parent then collapse")
	}
	p.Update(navKey("right"))
	p.Update(navKey("j"))
	updated := fixtureInventory()
	updated.Groups["web"].Hosts = []string{"web-b", "web-a"}
	p.SetInventory(updated)
	if p.SelectedHost() != "web-a" {
		t.Fatalf("refresh lost identity: %q", p.SelectedHost())
	}
}

func TestEmptyInventoryNavigationNeverSelects(t *testing.T) {
	p := NewInventoryPanel(&core.Inventory{}, 1, 1)
	p.SetFocused(true)
	for _, key := range []string{"G", "end", "enter", "s", "h", "l", "space", "g", "home"} {
		if cmd := p.Update(navKey(key)); cmd != nil {
			t.Fatalf("empty %s emitted action", key)
		}
		if p.SelectedHost() != "" || p.SelectedGroup() != "" {
			t.Fatal("empty inventory selected a target")
		}
		_ = p.View()
	}
}

func TestInventoryFilterOwnsTextAndHiddenSelection(t *testing.T) {
	p := NewInventoryPanel(fixtureInventory(), 30, 20)
	p.SetFocused(true)
	p.Update(navKey("/"))
	p.Update(navKey("jqkhl/?"))
	p.Update(navKey("space"))
	if p.filter.input.Value() != "jqkhl/? " {
		t.Fatalf("filter text = %q", p.filter.input.Value())
	}
	if p.SelectedHost() != "" || p.SelectedGroup() != "" {
		t.Fatal("filtered-empty retained hidden selection")
	}
	p.Update(navKey("enter"))
	if p.FilterActive() {
		t.Fatal("enter did not return to list")
	}
	if cmd := p.Update(navKey("enter")); cmd != nil {
		t.Fatal("filtered-empty inspection emitted action")
	}
	p.Update(navKey("esc"))
	if p.SelectedGroup() != "web" {
		t.Fatal("clear filter did not restore visible rows")
	}
}

func TestPlaybookInspectReviewSelectionAndFilter(t *testing.T) {
	books := []*core.Playbook{{Name: "same", Path: "/one/site.yml"}, {Name: "same", Path: "/two/site.yml"}}
	p := NewPlaybooksPanel(books, 16, 20)
	p.SetFocused(true)
	p.Update(navKey("end"))
	if got := p.Update(navKey("enter"))().(ViewPlaybookMsg); got.Playbook.Path != books[1].Path {
		t.Fatalf("inspect = %+v", got)
	}
	p.SetLimit("web")
	cmd := p.Update(navKey("r"))
	p.SetLimit("db")
	if got := cmd().(RunRequestMsg); got.Limit != "web" {
		t.Fatal("run request was not snapshotted")
	}
	p.SetPlaybooks([]*core.Playbook{books[1], books[0]})
	if p.SelectedPlaybook().Path != books[1].Path {
		t.Fatal("refresh lost playbook identity")
	}
	if p.SelectByNameUnique("same") {
		t.Fatal("ambiguous name must not select an arbitrary playbook")
	}
	p.Update(navKey("/"))
	p.Update(navKey("jqkhl/?"))
	if p.filter.input.Value() != "jqkhl/?" {
		t.Fatal("filter intercepted printable shortcuts")
	}
	if p.SelectedPlaybook() != nil {
		t.Fatal("filtered empty retained selected playbook")
	}
	p.Update(navKey("enter"))
	if cmd := p.Update(navKey("r")); cmd != nil {
		t.Fatal("filtered-empty run emitted action")
	}
	if !p.SelectByPath(books[0].Path) || p.SelectedPlaybook().Path != books[0].Path {
		t.Fatal("select path should reveal the explicit playbook")
	}
}

func TestPanelViewsHandleNarrowWidthsAndUnicode(t *testing.T) {
	for _, width := range []int{0, 1, 8, 16, 40, 76} {
		p := NewPlaybooksPanel([]*core.Playbook{{Name: "專案é👩🏽‍💻", Path: "/專案/👩🏽‍💻.yml", Hosts: []string{"web"}}}, width, 6)
		p.SetFocused(true)
		inv := NewInventoryPanel(fixtureInventory(), width, 6)
		status := NewStatusPanel(width, 6)
		status.UpdateHost("專案é👩🏽‍💻", core.TaskStatusOK, "處理")
		logs := NewLogsPanel(width, 6)
		logs.AddLine(core.LogLine{Text: "專案é👩🏽‍💻", Level: core.LogLevelInfo})
		for name, view := range map[string]string{"playbooks": p.View(), "inventory": inv.View(), "status": status.View(), "logs": logs.View()} {
			if !utf8.ValidString(view) {
				t.Fatalf("%s invalid UTF-8 at width %d", name, width)
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("%s line width %d > %d: %q", name, lipgloss.Width(line), width, line)
				}
			}
		}
		for _, text := range []string{"****", "TASK [專案] ****", "專案", "$ ansible-playbook 專案.yml"} {
			_ = renderLogLine(core.LogLine{Text: text, Level: core.LogLevelCommand}, width, true)
		}
	}
	if got := cellTruncate("👩🏽‍💻abc", 3); got != "👩🏽‍💻…" {
		t.Fatalf("grapheme truncation = %q", got)
	}
}

func TestStatusPreservesSelectionWhileResultsReorder(t *testing.T) {
	p := NewStatusPanel(40, 20)
	p.UpdateHost("a", core.TaskStatusOK, "one")
	p.UpdateHost("b", core.TaskStatusFailed, "two")
	if p.SelectedHost() != "a" {
		t.Fatal("incoming status changed selected host")
	}
	p.SetFocused(true)
	p.Update(navKey("home"))
	if p.SelectedHost() != "b" {
		t.Fatal("failed host should be first")
	}
	if got := p.Update(navKey("enter"))().(InspectInventoryMsg); got.Host != "b" {
		t.Fatalf("inspect = %+v", got)
	}
}

func TestLogSearchAcceptsSpacesAndPreviousMatch(t *testing.T) {
	p := NewLogsPanel(30, 6)
	p.SetFocused(true)
	p.AddLine(core.LogLine{Text: "j q"})
	p.AddLine(core.LogLine{Text: "j q"})
	p.Update(navKey("/"))
	p.Update(navKey("j"))
	p.Update(navKey("space"))
	p.Update(navKey("q"))
	p.Update(navKey("enter"))
	if p.SearchQuery() != "j q" || len(p.searchMatches) != 2 {
		t.Fatal("log search lost text or matches")
	}
	p.Update(navKey("N"))
	if p.matchCursor != 0 {
		t.Fatalf("previous match cursor = %d", p.matchCursor)
	}
}
