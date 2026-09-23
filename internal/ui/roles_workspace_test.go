package ui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/inventory"
)

func roleWorkspaceFixture(t *testing.T) (string, *core.Playbook) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "site.yml")
	body := "- name: Demo play\n  hosts: local\n  roles:\n    - role: demo\n      tags: [greeting]\n    - role: demo\n      tags: [summary]\n    - role: '{{ dynamic_role }}'\n      tags: [dynamic]\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	pb, ok := inventory.ParseSinglePlaybook(path)
	if !ok {
		t.Fatal("fixture parse")
	}
	return dir, pb
}
func writeRoleSources(t *testing.T, dir string) {
	t.Helper()
	for name, body := range map[string]string{"tasks/main.yml": "- name: Print demo\n  ansible.builtin.debug:\n    msg: hi\n", "defaults/main.yml": "message: role default\n"} {
		path := filepath.Join(dir, "roles", "demo", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func completeRoles(o *RolesOverlay, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			completeRoles(o, child)
		}
		return
	}
	o.Apply(msg)
}

func TestRolesLoadIsDeferredAndDeclarationsKeepDistinctContext(t *testing.T) {
	dir, pb := roleWorkspaceFixture(t)
	o := newRolesOverlay(90, 24)
	cmd := o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "inventory.ini", "localhost")
	if !o.loading || o.err != nil {
		t.Fatal("Open must schedule discovery")
	}
	// Created after Open: observing these files proves filesystem discovery is
	// inside the returned command, not inside an input handler.
	writeRoleSources(t, dir)
	completeRoles(o, cmd)
	if o.loading || o.err != nil || o.projectMode || len(o.visibleRows()) != 3 {
		t.Fatalf("load=%+v", o)
	}
	first := o.selectedRow()
	if first == nil || first.role == nil || first.declaration.PlayName != "Demo play" {
		t.Fatal("related role source/context missing")
	}
	if got := o.DeclaredTags(); !reflect.DeepEqual(got, []string{"greeting"}) {
		t.Fatal(got)
	}
	o.cursor = 1
	if got := o.DeclaredTags(); !reflect.DeepEqual(got, []string{"summary"}) {
		t.Fatal(got)
	}
	req, ok := o.StandaloneRequest()
	if !ok || req.RolePath != filepath.Join(dir, "roles", "demo") || req.Tags != "" {
		t.Fatalf("standalone inherited parent tags: %+v %v", req, ok)
	}
	o.cursor = 2
	if _, ok := o.StandaloneRequest(); ok || len(o.DeclaredTags()) != 0 {
		t.Fatal("dynamic declaration became runnable/static tags")
	}
	if !strings.Contains(o.ContentView(90, 24), "not resolved") {
		t.Fatal("unknown state not explained")
	}
}
func TestRolesScopeSwitchPreservesNavigationAndSourceStaysInWorkspace(t *testing.T) {
	dir, pb := roleWorkspaceFixture(t)
	writeRoleSources(t, dir)
	o := newRolesOverlay(90, 24)
	completeRoles(o, o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "", ""))
	o.cursor = 1
	o.pane = 1
	o.detailOff = 2
	o.filter.SetValue("play 1")
	o.Update(overlayKey("a"))
	if !o.projectMode || len(o.visibleRows()) != 1 || o.filter.Value() != "" {
		t.Fatal("project scope did not get own navigation state")
	}
	o.Update(overlayKey("a"))
	if o.projectMode || o.cursor != 1 || o.pane != 1 || o.detailOff != 2 || o.filter.Value() != "play 1" {
		t.Fatal("return lost declaration context")
	}
	o.Update(overlayKey("f"))
	if o.sourceMode != 1 || len(o.sourceFiles) != 3 || o.sourceFiles[0].Kind != "declaration" {
		t.Fatalf("source list=%+v", o.sourceFiles)
	}
	cmd := o.Update(overlayKey("enter"))
	if cmd == nil || o.sourceMode != 2 || !o.sourcePending || o.sourceContent != "" {
		t.Fatal("source preview did not defer read")
	}
	completeRoles(o, cmd)
	if !strings.Contains(o.sourceContent, "Demo play") || o.sourceError != "" {
		t.Fatal("declaration source missing")
	}
	if !o.HandleEscape() || o.sourceMode != 1 {
		t.Fatal("Esc should return to source list")
	}
	if !o.HandleEscape() || o.sourceMode != 0 || o.cursor != 1 || o.filter.Value() != "play 1" {
		t.Fatal("source return lost role selection")
	}
}
func TestRolesRejectStaleDiscoveryAndSourceResults(t *testing.T) {
	dir, pb := roleWorkspaceFixture(t)
	writeRoleSources(t, dir)
	o := newRolesOverlay(90, 24)
	first := o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "", "")
	nextDir, nextPB := roleWorkspaceFixture(t)
	second := o.Open(context.Background(), filepath.Join(nextDir, "roles"), nextPB, "", "")
	completeRoles(o, first)
	if !o.loading || len(o.roles) != 0 || o.playbook.Path != nextPB.Path {
		t.Fatal("old discovery replaced new project")
	}
	completeRoles(o, second)
	if o.err != nil || len(o.roles) != 0 {
		t.Fatal("missing role directory is a valid empty observation")
	}
	completeRoles(o, o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "", ""))
	o.Update(overlayKey("f"))
	old := o.Update(overlayKey("enter"))
	o.HandleEscape()
	o.sourceCursor = 1
	current := o.Update(overlayKey("enter"))
	completeRoles(o, old)
	if !o.sourcePending || o.sourceContent != "" {
		t.Fatal("late source replaced current selection")
	}
	completeRoles(o, current)
	if !strings.Contains(o.sourceContent, "Print demo") {
		t.Fatalf("source=%q", o.sourceContent)
	}
}
func TestRolesSourceRefreshPreservesSelectionAndRereads(t *testing.T) {
	dir, pb := roleWorkspaceFixture(t)
	writeRoleSources(t, dir)
	o := newRolesOverlay(90, 24)
	completeRoles(o, o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "", ""))
	o.cursor = 1
	o.Update(overlayKey("f"))
	o.sourceCursor = 1
	completeRoles(o, o.Update(overlayKey("enter")))
	path := o.sourceFiles[1].Path
	os.WriteFile(path, []byte("- name: Edited source\n  debug: {msg: changed}\n"), 0600)
	o.Invalidate()
	completeRoles(o, o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "", ""))
	if o.cursor != 1 || o.sourceMode != 2 || !strings.Contains(o.sourceContent, "Edited source") {
		t.Fatal("refresh lost view or did not reread source")
	}
}
func TestRolesViewBoundsAndDoesNotMutateNavigation(t *testing.T) {
	dir, pb := roleWorkspaceFixture(t)
	writeRoleSources(t, dir)
	pb.Name = "專案é👩🏽‍💻"
	o := newRolesOverlay(80, 24)
	completeRoles(o, o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "", ""))
	o.filter.SetValue("demo")
	o.detailOff = 999
	for _, size := range [][2]int{{140, 35}, {80, 18}, {50, 12}, {12, 5}, {1, 1}, {0, 0}} {
		before := roleNavigation{o.cursor, o.pane, o.detailOff, o.filter.Value()}
		filterWidth := o.filter.Width
		for _, pane := range []int{0, 1} {
			o.pane = pane
			rendered := o.ContentView(size[0], size[1])
			if !utf8.ValidString(rendered) || lipgloss.Width(rendered) > size[0] || lipgloss.Height(rendered) > max(1, size[1]) {
				t.Fatalf("size=%v pane=%d rendered=%q", size, pane, rendered)
			}
		}
		if o.cursor != before.cursor || o.detailOff != before.offset || o.filter.Value() != before.query || o.filter.Width != filterWidth {
			t.Fatal("View mutated navigation")
		}
	}
}

func TestRoleRefreshFollowsDeclarationIdentityAfterInsertion(t *testing.T) {
	dir, pb := roleWorkspaceFixture(t)
	writeRoleSources(t, dir)
	o := newRolesOverlay(90, 24)
	completeRoles(o, o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "", ""))
	before := o.selectedRow().id
	body, err := os.ReadFile(pb.Path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(body), "  roles:\n", "  roles:\n    - other_role\n", 1)
	if err := os.WriteFile(pb.Path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	o.Invalidate()
	completeRoles(o, o.Open(context.Background(), filepath.Join(dir, "roles"), pb, "", ""))
	if o.cursor != 1 || o.selectedRow().id != before || !reflect.DeepEqual(o.DeclaredTags(), []string{"greeting"}) {
		t.Fatal("refresh followed an old row number instead of declaration identity")
	}
}
