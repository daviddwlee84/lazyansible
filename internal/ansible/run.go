package ansible

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func normalizeProject(p ProjectContext) (ProjectContext, error) {
	var err error
	if p.WorkDir == "" {
		p.WorkDir, err = os.Getwd()
		if err != nil {
			return p, err
		}
	}
	p.WorkDir, err = filepath.Abs(p.WorkDir)
	if err != nil {
		return p, err
	}
	info, err := os.Stat(p.WorkDir)
	if err != nil {
		return p, fmt.Errorf("project directory: %w", err)
	}
	if !info.IsDir() {
		return p, errors.New("project workdir is not a directory")
	}
	if p.PlaybookDir != "" && !filepath.IsAbs(p.PlaybookDir) {
		p.PlaybookDir = filepath.Join(p.WorkDir, p.PlaybookDir)
	}
	return p, nil
}

// Companion resolves Ansible commands from one installation. An explicit
// executable must have the requested sibling; it never silently falls back to
// a different installation elsewhere on PATH.
func Companion(name, preferred string) (string, error) {
	if preferred == "" {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("%s not found in PATH", name)
		}
		return filepath.Abs(path)
	}
	path, err := exec.LookPath(preferred)
	if err != nil {
		return "", fmt.Errorf("Ansible executable %q: %w", preferred, err)
	}
	// Symlinked uv/Homebrew entrypoints must keep their companion binaries in
	// the same environment, even when another installation shadows PATH.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if filepath.Base(path) != name {
		path = filepath.Join(filepath.Dir(path), name)
	}
	if _, err = exec.LookPath(path); err != nil {
		return "", fmt.Errorf("%s is missing beside selected Ansible executable", name)
	}
	return filepath.Abs(path)
}

// Prepare validates inputs and returns exactly the command Execute will run.
// Role playbook contents are retained in memory until execution.
func Prepare(ctx context.Context, request RunRequest) (RunPlan, error) {
	if err := ctx.Err(); err != nil {
		return RunPlan{}, err
	}
	p, err := normalizeProject(request.Project)
	if err != nil {
		return RunPlan{}, err
	}
	request.Project = p
	if request.Kind == "" {
		request.Kind = "playbook"
	}
	plan := RunPlan{Request: request}
	binary := "ansible-playbook"
	args := []string{}
	switch request.Kind {
	case "playbook", "syntax", "list-tags", "list-hosts", "lint":
		if request.Playbook == "" {
			return plan, errors.New("a playbook is required")
		}
		playbook := request.Playbook
		if !filepath.IsAbs(playbook) {
			playbook = filepath.Join(p.WorkDir, playbook)
		}
		info, err := os.Stat(playbook)
		if err != nil {
			return plan, fmt.Errorf("playbook: %w", err)
		}
		if info.IsDir() {
			return plan, errors.New("playbook must be a file")
		}
		args = append(args, playbook)
		if request.Kind == "lint" {
			binary = "ansible-lint"
			args = []string{"--nocolor", playbook}
		}
	case "role":
		if request.RolePath == "" {
			return plan, errors.New("a role path is required")
		}
		role := request.RolePath
		if !filepath.IsAbs(role) {
			role = filepath.Join(p.WorkDir, role)
		}
		info, err := os.Stat(role)
		if err != nil {
			return plan, fmt.Errorf("role: %w", err)
		}
		if !info.IsDir() {
			return plan, errors.New("role path must be a directory")
		}
		hosts := request.Hosts
		if hosts == "" {
			hosts = "all"
		}
		plan.roleContent, err = yaml.Marshal([]map[string]any{{"name": "Run selected role", "hosts": hosts, "roles": []string{role}}})
		if err != nil {
			return plan, err
		}
		args = append(args, "<generated-role-playbook>")
	case "adhoc":
		binary = "ansible"
		hosts := request.Hosts
		if hosts == "" {
			hosts = "all"
		}
		if strings.HasPrefix(hosts, "-") {
			return plan, errors.New("host pattern must not begin with '-'")
		}
		if request.Module == "" {
			return plan, errors.New("an ad-hoc module is required")
		}
		args = []string{hosts, "-m", request.Module}
		if request.Args != "" {
			args = append(args, "-a", request.Args)
		}
	default:
		return plan, fmt.Errorf("unknown run kind %q", request.Kind)
	}
	if request.Kind != "lint" {
		if p.Inventory != "" {
			args = append(args, "-i", p.Inventory)
		}
		if request.Limit != "" {
			args = append(args, "--limit", request.Limit)
		}
		if request.Tags != "" && request.Kind != "adhoc" {
			args = append(args, "--tags", request.Tags)
		}
		if request.Check {
			args = append(args, "--check")
		}
		if request.Diff {
			args = append(args, "--diff")
		}
		if request.Become {
			args = append(args, "--become")
		}
		for _, value := range request.ExtraVars {
			args = append(args, "-e", value)
		}
		if request.VaultPasswordFile != "" {
			args = append(args, "--vault-password-file", request.VaultPasswordFile)
		}
		switch request.Kind {
		case "syntax":
			args = append(args, "--syntax-check")
		case "list-tags":
			args = append(args, "--list-tags")
		case "list-hosts":
			args = append(args, "--list-hosts")
		}
	}
	preferred := request.Executable
	if preferred == "" {
		preferred = p.Executable
	}
	// ansible-lint is an independent optional tool, not an ansible-core entrypoint.
	if request.Kind == "lint" {
		preferred = ""
	} else {
		plan.Runtime, err = Resolve(ctx, RuntimeOptions{Executable: preferred, WorkDir: p.WorkDir})
		if err != nil {
			return plan, err
		}
		preferred = plan.Runtime.Executable
	}
	executable, err := Companion(binary, preferred)
	if err != nil {
		return plan, err
	}
	plan.Command = CommandSpec{Executable: executable, Args: args, Dir: p.WorkDir, Env: append([]string{}, request.Env...)}
	if request.Kind != "lint" {
		plan.Command.Env = append(plan.Command.Env, "ANSIBLE_STDOUT_CALLBACK=default", "ANSIBLE_NOCOLOR=1", "ANSIBLE_FORCE_COLOR=0")
		plan.Callback = "default"
		plan.Notes = []string{"Execution uses the default Ansible callback with color disabled; project files are unchanged."}
	}
	plan.Preview = displayCommand(plan.Command)
	return plan, nil
}
