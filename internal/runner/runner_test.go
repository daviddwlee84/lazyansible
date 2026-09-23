package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
)

func TestStreamPlanBridgePreservesEventsAndExit(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-playbook")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'ok: [alpha]\\nfatal: [beta]: UNREACHABLE!\\n'\nprintf 'diagnostic\\n' >&2\nexit 4\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var messages []tea.Msg
	msg := StreamPlanCmd(context.Background(), ansible.RunPlan{Command: ansible.CommandSpec{Executable: binary, Dir: dir}}, func(m tea.Msg) { messages = append(messages, m) })()
	finished, ok := msg.(RunFinishedMsg)
	if !ok || finished.ExitCode != 4 || finished.Err == nil {
		t.Fatalf("exit lost: %+v", msg)
	}
	logs, hosts := 0, map[string]core.TaskStatus{}
	for _, msg := range messages {
		switch m := msg.(type) {
		case LogMsg:
			logs++
		case HostStatusMsg:
			hosts[m.Host] = m.Status
		}
	}
	if logs != 3 || hosts["alpha"] != core.TaskStatusOK || hosts["beta"] != core.TaskStatusUnreachable {
		t.Fatalf("bridge lost events: %d %+v", logs, hosts)
	}
}

func TestAdHocStatusAndPreviewRedaction(t *testing.T) {
	status, host, _, ok := parseHostStatus("localhost | SUCCESS => {}")
	if !ok || host != "localhost" || status != core.TaskStatusOK {
		t.Fatalf("ad hoc status not parsed: %v %q %v", status, host, ok)
	}
	preview := BuildAdHocCommand(core.AdHocOptions{Module: "shell", Args: "echo secret-token", ExtraVars: map[string]string{"password": "secret-password"}})
	if strings.Contains(preview, "secret-") {
		t.Fatalf("preview exposed argument values: %s", preview)
	}
}

func TestRecapOverridesIgnoredTaskFailure(t *testing.T) {
	status, host, task, ok := parseHostStatus("alpha : ok=2 changed=0 unreachable=0 failed=0 skipped=1 rescued=0 ignored=1")
	if !ok || host != "alpha" || status != core.TaskStatusOK || task != "PLAY RECAP" {
		t.Fatalf("ignored failure remained fatal: %v %q %q %v", status, host, task, ok)
	}
	status, _, _, ok = parseHostStatus("alpha : ok=2 changed=1 unreachable=0 failed=1 skipped=0 rescued=0 ignored=0")
	if !ok || status != core.TaskStatusFailed {
		t.Fatalf("real recap failure was hidden: %v %v", status, ok)
	}
	if _, _, _, ok = parseHostStatus("debug ok=2 changed=0 unreachable=0 failed=0"); ok {
		t.Fatal("non-recap text classified as host summary")
	}
}
