// Package runner executes Ansible playbooks and streams output.
package runner

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/core"
)

// LogMsg is sent over the Bubble Tea message bus for each log line.
type LogMsg struct {
	Line core.LogLine
}

// RunFinishedMsg is sent when the ansible-playbook process exits.
type RunFinishedMsg struct {
	ExitCode int
	Err      error
	Duration time.Duration
}

// HostStatusMsg is sent when a host status change is detected in output.
type HostStatusMsg struct {
	Host   string
	Status core.TaskStatus
	Task   string
}

// StreamCmd returns a Bubble Tea command that spawns ansible-playbook and streams
// output messages back through the tea.Program's Send channel.
func StreamCmd(ctx context.Context, opts core.RunOptions, sendFn func(tea.Msg)) tea.Cmd {
	return func() tea.Msg {
		request := ansible.RunRequest{Kind: "playbook", Project: ansible.ProjectContext{Inventory: opts.Inventory}, Playbook: opts.Playbook, Limit: opts.Limit, Tags: opts.Tags, Check: opts.CheckMode, Diff: opts.DiffMode, VaultPasswordFile: opts.VaultPasswordFile, Env: opts.Env}
		for _, k := range sortedVarKeys(opts.ExtraVars) {
			request.ExtraVars = append(request.ExtraVars, fmt.Sprintf("%s=%s", k, opts.ExtraVars[k]))
		}
		if opts.ExtraVarsRaw != "" {
			request.ExtraVars = append(request.ExtraVars, opts.ExtraVarsRaw)
		}
		plan, err := ansible.Prepare(ctx, request)
		if err != nil {
			return RunFinishedMsg{ExitCode: -1, Err: err}
		}
		return StreamPlanCmd(ctx, plan, sendFn)()
	}
}

// BuildPlaybookCommand returns the full command string that would be executed
// for the given RunOptions, suitable for display in the UI.
func BuildPlaybookCommand(opts core.RunOptions) string {
	args := buildPlaybookArgs(opts)
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "ansible-playbook")
	hideNext := false
	for _, a := range args {
		if hideNext {
			parts = append(parts, "<redacted>")
			hideNext = false
			continue
		}
		if a == "-e" || a == "-a" || a == "--vault-password-file" {
			hideNext = true
		}
		if strings.ContainsAny(a, " \t\"'") {
			parts = append(parts, "'"+strings.ReplaceAll(a, "'", `'"'"'`)+"'")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// BuildAdHocCommand returns the full ansible ad-hoc command string for display.
func BuildAdHocCommand(opts core.AdHocOptions) string {
	args := buildAdHocArgs(opts)
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "ansible")
	hideNext := false
	for _, a := range args {
		if hideNext {
			parts = append(parts, "<redacted>")
			hideNext = false
			continue
		}
		if a == "-e" || a == "-a" || a == "--vault-password-file" {
			hideNext = true
		}
		if strings.ContainsAny(a, " \t\"'") {
			parts = append(parts, "'"+strings.ReplaceAll(a, "'", `'"'"'`)+"'")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// AdHocStreamCmd runs an ansible ad-hoc command and streams output.
func AdHocStreamCmd(ctx context.Context, opts core.AdHocOptions, sendFn func(tea.Msg)) tea.Cmd {
	return func() tea.Msg {
		request := ansible.RunRequest{Kind: "adhoc", Project: ansible.ProjectContext{Inventory: opts.Inventory}, Hosts: opts.Hosts, Module: opts.Module, Args: opts.Args, Become: opts.Become}
		for _, k := range sortedVarKeys(opts.ExtraVars) {
			request.ExtraVars = append(request.ExtraVars, fmt.Sprintf("%s=%s", k, opts.ExtraVars[k]))
		}
		plan, err := ansible.Prepare(ctx, request)
		if err != nil {
			return RunFinishedMsg{ExitCode: -1, Err: err}
		}
		return StreamPlanCmd(ctx, plan, sendFn)()
	}
}

// CheckBinary returns an error if ansible-playbook is not found in PATH.
func CheckBinary() error {
	_, err := exec.LookPath("ansible-playbook")
	if err != nil {
		return fmt.Errorf("ansible-playbook not found in PATH: %w", err)
	}
	return nil
}

// CheckAdHocBinary returns an error if ansible is not found in PATH.
func CheckAdHocBinary() error {
	_, err := exec.LookPath("ansible")
	if err != nil {
		return fmt.Errorf("ansible not found in PATH: %w", err)
	}
	return nil
}

// CheckLintBinary returns an error if ansible-lint is not found in PATH.
func CheckLintBinary() error {
	_, err := exec.LookPath("ansible-lint")
	if err != nil {
		return fmt.Errorf("ansible-lint not found in PATH: %w", err)
	}
	return nil
}

// LintCmd runs ansible-lint on the given playbook path and streams output.
func LintCmd(ctx context.Context, playbookPath string, sendFn func(tea.Msg)) tea.Cmd {
	return func() tea.Msg {
		plan, err := ansible.Prepare(ctx, ansible.RunRequest{Kind: "lint", Playbook: playbookPath})
		if err != nil {
			return RunFinishedMsg{ExitCode: -1, Err: err}
		}
		return StreamPlanCmd(ctx, plan, sendFn)()
	}
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// StreamPlanCmd is the Bubble Tea bridge for a shared, reviewed domain plan.
func StreamPlanCmd(ctx context.Context, plan ansible.RunPlan, sendFn func(tea.Msg)) tea.Cmd {
	return func() tea.Msg {
		result, err := ansible.Execute(ctx, plan, func(event ansible.Event) {
			if sendFn == nil {
				return
			}
			sendFn(LogMsg{Line: classifyLine(event.Line)})
			if status, host, task, ok := parseHostStatus(event.Line); ok {
				sendFn(HostStatusMsg{Host: host, Status: status, Task: task})
			}
		})
		return RunFinishedMsg{ExitCode: result.ExitCode, Err: err, Duration: result.Duration}
	}
}

func sortedVarKeys(vars map[string]string) []string {
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func buildPlaybookArgs(opts core.RunOptions) []string {
	args := []string{opts.Playbook}
	if opts.Inventory != "" {
		args = append(args, "-i", opts.Inventory)
	}
	if opts.Limit != "" {
		args = append(args, "--limit", opts.Limit)
	}
	if opts.Tags != "" {
		args = append(args, "--tags", opts.Tags)
	}
	if opts.CheckMode {
		args = append(args, "--check")
	}
	if opts.DiffMode {
		args = append(args, "--diff")
	}
	keys := make([]string, 0, len(opts.ExtraVars))
	for k := range opts.ExtraVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, opts.ExtraVars[k]))
	}
	if opts.ExtraVarsRaw != "" {
		args = append(args, "-e", opts.ExtraVarsRaw)
	}
	if opts.VaultPasswordFile != "" {
		args = append(args, "--vault-password-file", opts.VaultPasswordFile)
	}
	return args
}

func buildAdHocArgs(opts core.AdHocOptions) []string {
	hosts := opts.Hosts
	if hosts == "" {
		hosts = "all"
	}
	args := []string{hosts}
	if opts.Inventory != "" {
		args = append(args, "-i", opts.Inventory)
	}
	args = append(args, "-m", opts.Module)
	if opts.Args != "" {
		args = append(args, "-a", opts.Args)
	}
	if opts.Become {
		args = append(args, "--become")
	}
	keys := make([]string, 0, len(opts.ExtraVars))
	for k := range opts.ExtraVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, opts.ExtraVars[k]))
	}
	return args
}

// classifyLine assigns a log level to a raw output line.
func classifyLine(text string) core.LogLine {
	lower := strings.ToLower(text)
	level := core.LogLevelInfo

	switch {
	case strings.HasPrefix(lower, "ok:"):
		level = core.LogLevelOK
	case strings.HasPrefix(lower, "changed:"):
		level = core.LogLevelChanged
	case strings.HasPrefix(lower, "failed:"), strings.HasPrefix(lower, "fatal:"):
		level = core.LogLevelFailed
	case strings.HasPrefix(lower, "warning:"), strings.Contains(lower, "[warning]"):
		level = core.LogLevelWarning
	case strings.Contains(lower, "task [") || strings.Contains(lower, "play ["):
		level = core.LogLevelInfo

	// ── Diff visualisation (--diff output) ────────────────────────────────
	case strings.HasPrefix(text, "--- ") || strings.HasPrefix(text, "+++ "):
		level = core.LogLevelDiffHeader
	case strings.HasPrefix(text, "@@"):
		level = core.LogLevelDiffHunk
	case len(text) > 0 && text[0] == '+':
		level = core.LogLevelDiffAdd
	case len(text) > 0 && text[0] == '-':
		level = core.LogLevelDiffRemove
	}

	return core.LogLine{
		Text:      text,
		Level:     level,
		Timestamp: time.Now(),
	}
}

// parseHostStatus extracts host/status from lines like:
//
//	ok: [hostname]
//	changed: [hostname]
//	failed: [hostname]
func parseHostStatus(text string) (status core.TaskStatus, host string, task string, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if status, host, ok := parseRecapStatus(text); ok {
		return status, host, "PLAY RECAP", true
	}
	// Ad-hoc commands use "host | SUCCESS =>" rather than playbook banners.
	if parts := strings.SplitN(text, " | ", 2); len(parts) == 2 {
		result := strings.ToLower(parts[1])
		s := core.TaskStatusUnknown
		switch {
		case strings.HasPrefix(result, "success"):
			s = core.TaskStatusOK
		case strings.HasPrefix(result, "changed"):
			s = core.TaskStatusChanged
		case strings.HasPrefix(result, "unreachable"):
			s = core.TaskStatusUnreachable
		case strings.HasPrefix(result, "failed"):
			s = core.TaskStatusFailed
		}
		if s != core.TaskStatusUnknown {
			return s, strings.TrimSpace(parts[0]), "", true
		}
	}

	var s core.TaskStatus
	switch {
	case (strings.HasPrefix(lower, "fatal:") || strings.HasPrefix(lower, "failed:")) && strings.Contains(lower, "unreachable"):
		s = core.TaskStatusUnreachable
	case strings.HasPrefix(lower, "ok:"):
		s = core.TaskStatusOK
	case strings.HasPrefix(lower, "changed:"):
		s = core.TaskStatusChanged
	case strings.HasPrefix(lower, "failed:"), strings.HasPrefix(lower, "fatal:"):
		s = core.TaskStatusFailed
	case strings.HasPrefix(lower, "skipping:"):
		s = core.TaskStatusSkipped
	case strings.Contains(lower, "unreachable"):
		s = core.TaskStatusUnreachable
	default:
		return 0, "", "", false
	}

	start := strings.Index(text, "[")
	end := strings.Index(text, "]")
	if start == -1 || end == -1 || end <= start {
		return 0, "", "", false
	}
	hostName := text[start+1 : end]
	return s, hostName, "", true
}

// Recap counters are authoritative for the final host state. A task failure
// followed by "...ignoring" must not leave that host marked failed when the
// recap reports failed=0, rescued/ignored>0.
func parseRecapStatus(text string) (core.TaskStatus, string, bool) {
	marker := strings.Index(text, "ok=")
	if marker < 0 {
		return 0, "", false
	}
	prefix := strings.TrimSpace(text[:marker])
	if !strings.HasSuffix(prefix, ":") {
		return 0, "", false
	}
	host := strings.TrimSpace(strings.TrimSuffix(prefix, ":"))
	if host == "" {
		return 0, "", false
	}
	counts := map[string]int{}
	for _, field := range strings.Fields(text[marker:]) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return 0, "", false
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return 0, "", false
		}
		counts[key] = n
	}
	for _, key := range []string{"ok", "changed", "unreachable", "failed"} {
		if _, ok := counts[key]; !ok {
			return 0, "", false
		}
	}
	switch {
	case counts["failed"] > 0:
		return core.TaskStatusFailed, host, true
	case counts["unreachable"] > 0:
		return core.TaskStatusUnreachable, host, true
	case counts["changed"] > 0:
		return core.TaskStatusChanged, host, true
	case counts["ok"] > 0:
		return core.TaskStatusOK, host, true
	case counts["skipped"] > 0:
		return core.TaskStatusSkipped, host, true
	default:
		return core.TaskStatusUnknown, host, true
	}
}
