package ui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/daviddwlee84/lazyansible/internal/galaxy"
	"github.com/daviddwlee84/lazyansible/internal/history"
	"github.com/daviddwlee84/lazyansible/internal/roles"
	"github.com/daviddwlee84/lazyansible/internal/runprofiles"
)

func overlayKey(value string) tea.KeyMsg {
	keys := map[string]tea.KeyType{"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "up": tea.KeyUp, "down": tea.KeyDown, "home": tea.KeyHome, "end": tea.KeyEnd}
	if value == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	if kind, ok := keys[value]; ok {
		return tea.KeyMsg{Type: kind}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func typeOverlay(update func(tea.Msg) tea.Cmd, text string) {
	for _, char := range text {
		update(overlayKey(string(char)))
	}
}

func TestTagsFilterOwnsPrintableKeys(t *testing.T) {
	o := newTagsOverlay(80, 24)
	o.SetTags([]string{"jqkhl/? aA", "web"})
	o.SetSelectedTags("web")
	o.Update(overlayKey("/"))
	typeOverlay(o.Update, "jqkhl/? aA")
	if o.filter.Value() != "jqkhl/? aA" || len(o.visible) != 1 {
		t.Fatalf("filter = %q; visible = %v", o.filter.Value(), o.visible)
	}
	if o.SelectedTagsString() != "web" {
		t.Fatal("typing selected/deselected tags")
	}
	if cmd := o.Update(overlayKey("enter")); cmd != nil {
		t.Fatal("filter enter must not confirm tags")
	}
	o.Update(overlayKey(" "))
	cmd := o.Update(overlayKey("enter"))
	if got := cmd().(TagsConfirmedMsg); got.Tags != "jqkhl/? aA,web" {
		t.Fatalf("tags = %q", got.Tags)
	}
	if !o.HandleEscape() || o.filter.Value() != "" {
		t.Fatal("Esc should clear retained filter first")
	}
	if o.HandleEscape() {
		t.Fatal("Esc should close when no nested interaction remains")
	}
}

func TestAdHocArgsRetainSpacesAndShortcutLetters(t *testing.T) {
	o := newAdHocOverlay(80, 24)
	o.SetTarget("web", "/tmp/inventory")
	o.Update(overlayKey("tab"))
	text := "cmd='echo jqkh/? hello world'"
	typeOverlay(o.Update, text)
	if o.argsInput.Value() != text {
		t.Fatalf("args = %q", o.argsInput.Value())
	}
	if got := o.Update(overlayKey("enter"))().(AdHocRunMsg); got.Opts.Args != text || got.Opts.Hosts != "web" {
		t.Fatalf("request = %+v", got)
	}
}

func TestRoleEnterInspectsAndFilterDoesNotRun(t *testing.T) {
	o := newRolesOverlay(80, 24)
	o.projectMode = true
	o.roles = []*roles.Role{{Name: "jqkhl/? role", Path: "/roles/one"}, {Name: "web", Path: "/roles/web"}}
	if cmd := o.Update(overlayKey("enter")); cmd != nil || o.pane != 1 {
		t.Fatal("Enter should inspect, never run")
	}
	if !o.HandleEscape() || o.pane != 0 {
		t.Fatal("Esc should restore role list")
	}
	o.Update(overlayKey("/"))
	typeOverlay(o.Update, "jqkhl/? role")
	if o.filter.Value() != "jqkhl/? role" || o.selected() == nil || o.selected().Path != "/roles/one" {
		t.Fatalf("role filter = %q", o.filter.Value())
	}
	if cmd := o.Update(overlayKey("enter")); cmd != nil {
		t.Fatal("filter enter must not emit a run")
	}
	if cmd := o.Update(overlayKey("r")); cmd != nil {
		t.Fatal("role model must not map r to standalone execution")
	}
	if got, ok := o.StandaloneRequest(); !ok || got.RolePath != "/roles/one" || got.Tags != "" {
		t.Fatalf("explicit standalone request=%+v ok=%v", got, ok)
	}
	o.Update(overlayKey("/"))
	typeOverlay(o.Update, "missing")
	o.Update(overlayKey("enter"))
	if _, ok := o.StandaloneRequest(); ok {
		t.Fatal("empty filtered roles must not run hidden selection")
	}
}

func TestHistoryAndProfileEnterInspect(t *testing.T) {
	record := &history.Record{ID: "1", Kind: "playbook", PlaybookName: "site", StartTime: time.Now(), EndTime: time.Now()}
	h := &HistoryOverlay{records: []*history.Record{record}, width: 80, height: 24}
	if cmd := h.Update(overlayKey("enter")); cmd != nil || !h.detail {
		t.Fatal("history Enter must only inspect")
	}
	if got := h.Update(overlayKey("r"))().(HistoryRunMsg); got.Record.ID != "1" {
		t.Fatal("history r should request review")
	}
	if !h.HandleEscape() || h.detail {
		t.Fatal("history Esc did not restore list")
	}
	p := &RunProfilesOverlay{profiles: []runprofiles.Profile{{Name: "staging", Playbook: "/site.yml", WorkDir: "/project"}}, width: 80, height: 24}
	if cmd := p.Update(overlayKey("enter")); cmd != nil || p.mode != rpModeDetail {
		t.Fatal("profile Enter must only inspect")
	}
	if got := p.Update(overlayKey("a"))().(RunProfileLoadMsg); got.Profile.WorkDir != "/project" {
		t.Fatal("profile apply lost project context")
	}
	if !p.HandleEscape() || p.mode != rpModeList {
		t.Fatal("profile Esc did not restore list")
	}
}

func TestGalaxyFilterAndLateLoadPreserveInput(t *testing.T) {
	g := newGalaxyOverlay(80, 24)
	g.roles = []galaxy.Item{{Name: "jqkhl/? role"}}
	g.Update(overlayKey("/"))
	typeOverlay(g.Update, "jqkhl/? role")
	if g.filterQuery != "jqkhl/? role" || len(g.filteredList()) != 1 {
		t.Fatalf("galaxy filter = %q", g.filterQuery)
	}
	if !g.HandleEscape() || g.filterActive {
		t.Fatal("Esc should return to list")
	}
	if !g.HandleEscape() || g.filterQuery != "" {
		t.Fatal("second Esc should clear filter")
	}
	g.Update(overlayKey("i"))
	typeOverlay(g.Update, "jqkhl/?")
	g.Update(galaxyLoadedMsg{roles: g.roles})
	if g.mode != galaxyModeInstall || g.input.Value() != "jqkhl/?" {
		t.Fatal("late discovery replaced an active install form")
	}
}

func TestOverlayTinyRenderingDoesNotPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, size := range [][2]int{{80, 24}, {40, 12}, {1, 1}, {0, 0}} {
		w, h := size[0], size[1]
		role := newRolesOverlay(w, h)
		role.roles = []*roles.Role{{Name: "專案é👩🏽‍💻", Path: "/roles/one", Tasks: []roles.Task{{Name: "處理", Module: "ansible.builtin.debug"}}}}
		historyView := &HistoryOverlay{records: []*history.Record{{PlaybookName: "專案", StartTime: time.Now(), EndTime: time.Now()}}, width: w, height: h}
		profiles := &RunProfilesOverlay{profiles: []runprofiles.Profile{{Name: "專案", Playbook: "/site.yml"}}, width: w, height: h}
		viewer := newPlaybookViewerOverlay(w, h)
		viewer.lines = []string{"- name: 專案é👩🏽‍💻"}
		g := newGalaxyOverlay(w, h)
		g.roles = []galaxy.Item{{Name: "專案"}}
		views := []string{role.View(), historyView.View(), profiles.View(), viewer.View(), g.View(), newSSHProfileOverlay(w, h).View(), newAdHocOverlay(w, h).View(), newTagsOverlay(w, h).View(), newEnvSwitchOverlay(w, h).View()}
		for _, view := range views {
			if !utf8.ValidString(view) {
				t.Fatal("overlay returned invalid UTF-8")
			}
		}
	}
	for width := 0; width < 10; width++ {
		for _, line := range strings.Split(yamlHighlight("name: 專案é👩🏽‍💻", width), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("YAML width %d exceeds %d", lipgloss.Width(line), width)
			}
		}
	}
}
