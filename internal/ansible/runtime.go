package ansible

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var coreVersionPattern = regexp.MustCompile(`\[core ([^\]]+)\]`)
var toolHeaderPattern = regexp.MustCompile(`^([A-Za-z0-9_.-]+) v([^ ]+)(.*?) \((.*)\)$`)
var constraintPattern = regexp.MustCompile(`\[required: (.*?)\]`)
var packageSpecPattern = regexp.MustCompile(`^(ansible-core|ansible)(\s*(==|!=|>=|<=|~=|>|<)\s*[A-Za-z0-9.*+_-]+(\s*,\s*(==|!=|>=|<=|~=|>|<)\s*[A-Za-z0-9.*+_-]+)*)?$`)

type installedTool struct{ Name, Version, Constraint, Dir string }

func parseTools(data string) ([]installedTool, error) {
	var result []installedTool
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "- ") {
			continue
		}
		m := toolHeaderPattern.FindStringSubmatch(line)
		if m == nil {
			return nil, errors.New("unsupported uv tool list output")
		}
		if m[1] != "ansible" && m[1] != "ansible-core" {
			continue
		}
		t := installedTool{Name: m[1], Version: m[2], Dir: m[4]}
		if c := constraintPattern.FindStringSubmatch(m[3]); c != nil {
			t.Constraint = c[1]
		}
		result = append(result, t)
	}
	return result, nil
}

func canonical(path string) string {
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	abs, err := filepath.Abs(p)
	if err == nil {
		return abs
	}
	return p
}

// Resolve reports the executable actually selected and confirms uv ownership
// against that tool environment, rather than trusting executable names on PATH.
func Resolve(ctx context.Context, opts RuntimeOptions) (RuntimeStatus, error) {
	s := RuntimeStatus{Ownership: "external"}
	provider := opts.Provider
	if provider == "" {
		provider = "auto"
	}
	if provider != "auto" && provider != "path" && provider != "uv" {
		return s, fmt.Errorf("unknown runtime provider %q", provider)
	}
	preferred := opts.Executable
	if preferred == "" {
		preferred, _ = exec.LookPath("ansible-playbook")
	}
	executable, ansibleErr := Companion("ansible", preferred)
	uv := opts.UVExecutable
	if uv == "" {
		uv = "uv"
	}
	uvPath, uvErr := exec.LookPath(uv)
	var tools []installedTool
	if uvErr == nil && provider != "path" {
		s.UVExecutable, _ = filepath.Abs(uvPath)
		out, _, err := capture(ctx, CommandSpec{Executable: s.UVExecutable, Args: []string{"--version"}}, 5*time.Second)
		if err == nil {
			s.UVVersion = strings.TrimSpace(string(out))
		}
		out, diagnostic, err := capture(ctx, CommandSpec{Executable: s.UVExecutable, Args: []string{"tool", "list", "--show-paths", "--show-version-specifiers", "--show-python", "--color", "never", "--no-progress"}}, 10*time.Second)
		if err != nil {
			s.Issue = fmt.Sprintf("cannot inspect uv ownership: %v: %s", err, diagnostic)
			s.Ownership = "unknown"
		} else if tools, err = parseTools(string(out)); err != nil {
			s.Issue = err.Error()
			s.Ownership = "unknown"
		}
	}
	if provider == "uv" {
		owner := opts.Package
		if owner == "" {
			owner = "ansible-core"
		}
		ansibleErr = fmt.Errorf("uv tool %s is not installed", owner)
		for _, tool := range tools {
			if tool.Name == owner {
				executable, ansibleErr = Companion("ansible", filepath.Join(tool.Dir, "bin", "ansible"))
				break
			}
		}
	}
	if ansibleErr != nil {
		s.Issue = ansibleErr.Error()
		return s, ansibleErr
	}
	s.Executable = executable
	s.PlaybookExecutable, _ = Companion("ansible-playbook", executable)
	for _, tool := range tools {
		owned := filepath.Join(tool.Dir, "bin", "ansible")
		if _, err := os.Stat(owned); err == nil && canonical(owned) == canonical(executable) {
			s.Managed = true
			s.Ownership = "uv"
			s.ToolPackage = tool.Name
			s.ToolVersion = tool.Version
			s.Constraint = redact(tool.Constraint)
			s.ToolDir = tool.Dir
			for _, name := range []string{"python3", "python"} {
				path := filepath.Join(tool.Dir, "bin", name)
				if _, err := os.Stat(path); err == nil {
					s.Python = path
					break
				}
			}
			break
		}
	}
	out, diagnostic, err := capture(ctx, CommandSpec{Executable: executable, Args: []string{"--version"}, Dir: opts.WorkDir}, 10*time.Second)
	if err != nil {
		return s, fmt.Errorf("inspect Ansible version: %w: %s", err, diagnostic)
	}
	if m := coreVersionPattern.FindSubmatch(out); m != nil {
		s.CoreVersion = string(m[1])
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "python version = ") {
			value := strings.TrimPrefix(line, "python version = ")
			fields := strings.Fields(value)
			if len(fields) > 0 {
				s.PythonVersion = fields[0]
			}
			if s.Python == "" {
				if i := strings.LastIndex(value, "("); i >= 0 && strings.HasSuffix(value, ")") {
					s.Python = value[i+1 : len(value)-1]
				}
			}
		}
	}
	if !s.Managed && s.Issue == "" {
		s.Issue = "selected Ansible executable is not owned by a recognized uv tool"
	}
	return s, nil
}

func Status(ctx context.Context, opts RuntimeOptions) (RuntimeStatus, error) {
	return Resolve(ctx, opts)
}

func PrepareInstall(ctx context.Context, opts RuntimeOptions, packageSpec string) (RunPlan, error) {
	if err := ctx.Err(); err != nil {
		return RunPlan{}, err
	}
	if packageSpec == "" {
		packageSpec = opts.Package
		if packageSpec == "" {
			packageSpec = "ansible-core"
		}
		packageSpec += opts.Constraint
	}
	name := strings.FieldsFunc(packageSpec, func(r rune) bool { return strings.ContainsRune("<>=!~ ", r) })
	if len(name) == 0 || !packageSpecPattern.MatchString(packageSpec) {
		return RunPlan{}, errors.New("package must be ansible-core or ansible with an optional version constraint")
	}
	uv := opts.UVExecutable
	if uv == "" {
		uv = "uv"
	}
	path, err := exec.LookPath(uv)
	if err != nil {
		return RunPlan{}, errors.New("uv is required to install Ansible; install uv first")
	}
	path, _ = filepath.Abs(path)
	args := []string{"tool", "install", packageSpec}
	if name[0] == "ansible" {
		args = append(args, "--with-executables-from", "ansible-core")
	}
	if opts.Python != "" {
		args = append(args, "--python", opts.Python)
	}
	plan := RunPlan{Command: CommandSpec{Executable: path, Args: args}, Request: RunRequest{Kind: "runtime-install"}}
	copy := opts
	plan.installOptions = &copy
	plan.installOwner = name[0]
	if current, err := Resolve(ctx, opts); err == nil {
		plan.Runtime = current
		if !current.Managed {
			plan.Notes = []string{"The selected Ansible executable has another owner. uv will not force replacement; active ownership will be verified after installation."}
		}
	}
	plan.Preview = displayCommand(plan.Command)
	return plan, nil
}

func PrepareUpgrade(ctx context.Context, opts RuntimeOptions) (RunPlan, error) {
	s, err := Resolve(ctx, opts)
	if err != nil {
		return RunPlan{}, err
	}
	if !s.Managed {
		return RunPlan{}, errors.New("selected Ansible is not uv-managed; use its owning package manager")
	}
	plan := RunPlan{Command: CommandSpec{Executable: s.UVExecutable, Args: []string{"tool", "upgrade", s.ToolPackage}}, Runtime: s, Request: RunRequest{Kind: "runtime-upgrade"}}
	copy := opts
	plan.runtimeOptions = &copy
	plan.Preview = displayCommand(plan.Command)
	return plan, nil
}

func Install(ctx context.Context, opts RuntimeOptions, spec string, emit func(Event)) (Result, error) {
	p, err := PrepareInstall(ctx, opts, spec)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	return Execute(ctx, p, emit)
}
func Upgrade(ctx context.Context, opts RuntimeOptions, emit func(Event)) (Result, error) {
	p, err := PrepareUpgrade(ctx, opts)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	return Execute(ctx, p, emit)
}
