package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
	"github.com/daviddwlee84/lazyansible/internal/roles"
	"github.com/muesli/termenv"
)

// Clipboard/title OSC, erase/cursor CSI, and an externally supplied blink style.
const terminalInjection = "\x1b]52;c;Y2xpcGJvYXJk\a\x1b]0;injected-title\x1b\\\x1b[2J\x1b[1;1H\x1b[5m"

var terminalSGR = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func assertTerminalDisplay(t *testing.T, view string, styled bool) {
	t.Helper()
	if strings.Contains(view, "\x1b[5m") {
		t.Fatalf("external terminal command leaked into view: %q", view)
	}
	for _, r := range terminalSGR.ReplaceAllString(view, "") {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("non-style terminal control %U leaked into view: %q", r, view)
		}
	}
	if styled && !terminalSGR.MatchString(view) {
		t.Fatal("application styles were stripped with external terminal controls")
	}
}

func terminalColorProfile(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func TestPlainTerminalTextPreservesSourceTextAndUnicode(t *testing.T) {
	input := terminalInjection + "專案 é 👩🏽‍💻\x1b[0m\n\tpassword: literal-secret\r\b\a\x00\u0085"
	want := "專案 é 👩🏽‍💻\n    password: literal-secret"
	if got := plainTerminalText(input); got != want {
		t.Fatalf("sanitized source = %q, want %q", got, want)
	}
	if got := plainTerminalLine(input); got != strings.ReplaceAll(want, "\n", " ") {
		t.Fatalf("single line = %q", got)
	}
	if !strings.HasPrefix(input, terminalInjection) {
		t.Fatal("source value was mutated")
	}
}

func TestTerminalInputViewDoesNotChangeValueOrCursor(t *testing.T) {
	terminalColorProfile(t)
	input := textinput.New()
	input.Width = 100
	input.SetValue("filter " + terminalInjection + "角色")
	input.SetCursor(len([]rune(input.Value())) - 1)
	input.Focus()
	value, position := input.Value(), input.Position()
	view := terminalInputView(input)
	assertTerminalDisplay(t, view, true)
	if input.Value() != value || input.Position() != position {
		t.Fatal("display sanitization changed editable filter state")
	}
}

func TestTagsDisplaySanitizesControlsWithoutChangingRunTags(t *testing.T) {
	terminalColorProfile(t)
	tag := "deploy" + terminalInjection + "角色"
	o := newTagsOverlay(80, 20)
	o.SetTags([]string{tag})
	o.SetSelectedTags(tag)
	o.title = "site" + terminalInjection
	o.notice = "Observed " + terminalInjection + "one tag"
	view := o.View()
	assertTerminalDisplay(t, view, true)
	if !strings.Contains(ansi.Strip(view), "deploy角色") {
		t.Fatal("readable tag label is missing")
	}
	if o.SelectedTagsString() != tag || o.allTags[0] != tag || !o.selected[tag] {
		t.Fatal("display sanitization changed the tag identity")
	}
	cmd := o.Update(overlayKey("enter"))
	if cmd == nil || cmd().(TagsConfirmedMsg).Tags != tag {
		t.Fatal("tag confirmation must retain the exact original Ansible tag value")
	}
}

func TestRolesDisplaySanitizesMetadataAndSourceWithoutRedactingText(t *testing.T) {
	terminalColorProfile(t)
	role := &roles.Role{Name: "demo" + terminalInjection, Path: "roles/demo" + terminalInjection, Desc: "Description " + terminalInjection, Tasks: []roles.Task{{Name: "Task " + terminalInjection, Module: "debug" + terminalInjection}}}
	o := newRolesOverlay(120, 30)
	o.projectMode = true
	o.roles = []*roles.Role{role}
	o.playbook = &core.Playbook{Name: "site" + terminalInjection}
	assertTerminalDisplay(t, o.View(), true)
	o.pane = 1
	assertTerminalDisplay(t, o.ContentView(60, 30), true)
	o.sourceTitle = "demo" + terminalInjection
	o.sourceFiles = []roles.SourceFile{{Kind: "tasks" + terminalInjection, Path: "main.yml" + terminalInjection}}
	o.sourceMode = 1
	assertTerminalDisplay(t, o.View(), true)
	o.sourceMode = 2
	o.sourceContent = "name: " + terminalInjection + "Readable\npassword: literal-secret\n\tkey: value"
	content := o.sourceContent
	view := o.View()
	assertTerminalDisplay(t, view, true)
	if !strings.Contains(ansi.Strip(view), "password: literal-secret") || !strings.Contains(ansi.Strip(view), "Readable") {
		t.Fatal("source preview should preserve actual source text apart from terminal controls")
	}
	if o.sourceContent != content || role.Name != "demo"+terminalInjection {
		t.Fatal("rendering changed stored role/source data")
	}
	o.sourceError = "failed " + terminalInjection
	assertTerminalDisplay(t, o.View(), true)
}

func TestWorkspaceAndReviewSanitizeRawFieldsBeforeStyling(t *testing.T) {
	terminalColorProfile(t)
	req := ansible.RunRequest{
		Kind: "playbook", Playbook: "site" + terminalInjection + ".yml", Tags: "deploy" + terminalInjection,
		Limit:   "local" + terminalInjection,
		Project: ansible.ProjectContext{WorkDir: "/project" + terminalInjection, Inventory: "inventory" + terminalInjection},
	}
	assertTerminalDisplay(t, renderWorkspaceContext(req, false), true)
	if req.Tags != "deploy"+terminalInjection {
		t.Fatal("workspace changed requested tags")
	}
	a := workbenchFixture(t)
	a.statusMsg = "Ready " + terminalInjection
	assertTerminalDisplay(t, a.workspaceFeedback(), false)
	// Only deliver a prepared result; no command or host operation is executed.
	a.prepareRun(req)
	plan := ansible.RunPlan{Request: req, Preview: "ansible-playbook " + terminalInjection + "site.yml", Command: ansible.CommandSpec{Executable: "ansible" + terminalInjection, Dir: req.Project.WorkDir}, Runtime: ansible.RuntimeStatus{CoreVersion: "2.19" + terminalInjection}}
	a.acceptRunPlan(runPreparedMsg{id: a.reviewID, plan: plan})
	assertTerminalDisplay(t, a.review.content, true)
	assertTerminalDisplay(t, a.reviewView(), true)
	if a.review.plan.Request.Tags != req.Tags || a.review.plan.Preview != plan.Preview {
		t.Fatal("review rendering changed the prepared plan")
	}
	a.review.err = errors.New("Cannot prepare " + terminalInjection + "operation").Error()
	a.syncReviewLayout()
	assertTerminalDisplay(t, a.reviewView(), true)
}

func TestPreviewSanitizesRequestAndObservedTextBeforeStyling(t *testing.T) {
	terminalColorProfile(t)
	a := workbenchFixture(t)
	a.openWorkspace(workspacePreview)
	p := &a.executionPreview
	p.result = &ansible.ExecutionPreview{
		Request: ansible.RunRequest{Playbook: "site" + terminalInjection + ".yml", Tags: "deploy" + terminalInjection},
		Parsed:  true, Command: "ansible-playbook " + terminalInjection, Output: "output " + terminalInjection,
		Diagnostics: "warning " + terminalInjection,
		Plays:       []ansible.PreviewPlay{{Name: "play" + terminalInjection, Pattern: "all" + terminalInjection, Hosts: []string{"local" + terminalInjection}, Tags: []string{"deploy" + terminalInjection}, Tasks: []ansible.PreviewTask{{Name: "task" + terminalInjection, Tags: []string{"deploy" + terminalInjection}}}}},
	}
	p.stale = true
	p.detail = true
	for section := range previewSections {
		p.section = section
		assertTerminalDisplay(t, a.previewDetailContent(), false)
		a.syncPreviewWorkspaceLayout()
		assertTerminalDisplay(t, a.previewWorkspaceView(), true)
	}
	if p.result.Request.Tags != "deploy"+terminalInjection || p.result.Output != "output "+terminalInjection {
		t.Fatal("preview rendering changed the observed data")
	}
}
