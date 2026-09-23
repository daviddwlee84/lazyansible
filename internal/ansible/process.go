package ansible

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const maxProbeOutput = 4 << 20

type boundedBuffer struct {
	buffer bytes.Buffer
	max    int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > b.max {
		return 0, fmt.Errorf("command output exceeded %d bytes", b.max)
	}
	return b.buffer.Write(p)
}

func (b *boundedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *boundedBuffer) String() string { return b.buffer.String() }

func mergeEnv(extra []string) []string {
	env := append([]string{}, os.Environ()...)
	for _, value := range extra {
		key, _, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		prefix := key + "="
		kept := env[:0]
		for _, old := range env {
			if !strings.HasPrefix(old, prefix) {
				kept = append(kept, old)
			}
		}
		env = append(kept, value)
	}
	return env
}

func command(ctx context.Context, spec CommandSpec) *exec.Cmd {
	cmd := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = mergeEnv(spec.Env)
	configureProcess(cmd)
	return cmd
}

func capture(ctx context.Context, spec CommandSpec, timeout time.Duration) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(ctx, spec)
	out := &boundedBuffer{max: maxProbeOutput}
	diagnostic := &boundedBuffer{max: maxProbeOutput}
	cmd.Stdout = out
	cmd.Stderr = diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.Bytes(), redact(ansi.Strip(diagnostic.String())), err
}

// Observe captures a bounded read-only command, keeping warnings out of JSON.
// Callers supply the selected installation and project directory explicitly.
func Observe(ctx context.Context, spec CommandSpec, timeout time.Duration) ([]byte, string, error) {
	return capture(ctx, spec, timeout)
}

var credentialURL = regexp.MustCompile("https?://[^\\s<>\"'`]+")

func redact(text string) string {
	return credentialURL.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "https://<redacted>"
		}
		if u.User != nil {
			u.User = url.User("redacted")
		}
		if u.RawQuery != "" {
			u.RawQuery = "redacted"
		}
		return u.String()
	})
}

// Execute runs a prepared command without invoking a shell. Callback calls are
// serialized even when stdout and stderr are active simultaneously.
func Execute(ctx context.Context, plan RunPlan, emit func(Event)) (Result, error) {
	start := time.Now()
	result := Result{ExitCode: -1}
	if plan.runtimeOptions != nil {
		current, err := Resolve(ctx, *plan.runtimeOptions)
		if err != nil {
			return result, err
		}
		if !current.Managed || current.ToolPackage != plan.Runtime.ToolPackage || current.ToolDir != plan.Runtime.ToolDir || current.ToolVersion != plan.Runtime.ToolVersion || canonical(current.Executable) != canonical(plan.Runtime.Executable) {
			return result, errors.New("Ansible ownership or version changed after review; prepare the upgrade again")
		}
	}
	spec := plan.Command
	if len(plan.roleContent) > 0 {
		f, err := os.CreateTemp("", "lazyansible-role-*.yml")
		if err != nil {
			return result, err
		}
		name := f.Name()
		defer os.Remove(name)
		if _, err = f.Write(plan.roleContent); err != nil {
			f.Close()
			return result, err
		}
		if err = f.Close(); err != nil {
			return result, err
		}
		spec.Args = append([]string{}, spec.Args...)
		for i, arg := range spec.Args {
			if arg == "<generated-role-playbook>" {
				spec.Args[i] = name
			}
		}
	}
	if spec.Executable == "" {
		return result, errors.New("plan has no executable")
	}
	cmd := command(ctx, spec)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return result, err
	}
	if err = cmd.Start(); err != nil {
		return result, fmt.Errorf("start %s: %w", spec.Executable, err)
	}
	var emitMu sync.Mutex
	done := make(chan error, 2)
	read := func(r io.Reader, stream string) {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), maxProbeOutput)
		for scanner.Scan() {
			if emit != nil {
				emitMu.Lock()
				emit(Event{Line: redact(ansi.Strip(scanner.Text())), Stream: stream})
				emitMu.Unlock()
			}
		}
		done <- scanner.Err()
	}
	go read(stdout, "stdout")
	go read(stderr, "stderr")
	first := <-done
	if first != nil {
		_ = cmd.Cancel()
	}
	second := <-done
	err = cmd.Wait()
	result.Duration = time.Since(start)
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if first != nil {
		return result, fmt.Errorf("read process output: %w", first)
	}
	if second != nil {
		return result, fmt.Errorf("read process output: %w", second)
	}
	if err != nil {
		return result, fmt.Errorf("%s exited with status %d: %w", spec.Executable, result.ExitCode, err)
	}
	if plan.installOptions != nil {
		active, verifyErr := Resolve(ctx, *plan.installOptions)
		if verifyErr != nil || !active.Managed || active.ToolPackage != plan.installOwner {
			return result, errors.New("uv completed installation, but the selected Ansible executable is not owned by that tool; check PATH or the configured executable before using it")
		}
	}
	return result, nil
}

func displayCommand(spec CommandSpec) string {
	args := make([]string, 0, len(spec.Args)+1)
	args = append(args, spec.Executable)
	hideNext := false
	for _, arg := range spec.Args {
		if hideNext {
			args = append(args, "<redacted>")
			hideNext = false
			continue
		}
		args = append(args, redact(arg))
		if arg == "-e" || arg == "--extra-vars" || arg == "-a" || arg == "--args" || arg == "--vault-password-file" {
			hideNext = true
		}
	}
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'`$;|&()<>*") {
			args[i] = "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
		}
	}
	return strings.Join(args, " ")
}
