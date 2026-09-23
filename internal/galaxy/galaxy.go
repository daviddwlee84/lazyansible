// Package galaxy wraps the ansible-galaxy CLI.
package galaxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
)

// Item represents a role or collection returned by list/search.
type Item struct {
	Name        string
	Version     string
	Description string
}

var operationContext struct {
	sync.RWMutex
	project ansible.ProjectContext
	runtime ansible.RuntimeOptions
}

// SetContext selects the same project and installation used by the workbench.
// Operations snapshot it so changing profiles cannot alter an in-flight call.
func SetContext(project ansible.ProjectContext, runtime ansible.RuntimeOptions) {
	operationContext.Lock()
	defer operationContext.Unlock()
	operationContext.project, operationContext.runtime = project, runtime
}

func commandSpec(ctx context.Context, args []string) (ansible.CommandSpec, error) {
	operationContext.RLock()
	project, options := operationContext.project, operationContext.runtime
	operationContext.RUnlock()
	if options.Executable == "" {
		options.Executable = project.Executable
	}
	runtime, err := ansible.Resolve(ctx, options)
	if err != nil {
		return ansible.CommandSpec{}, err
	}
	binary, err := ansible.Companion("ansible-galaxy", runtime.Executable)
	if err != nil {
		return ansible.CommandSpec{}, err
	}
	if project.WorkDir == "" {
		project.WorkDir, err = os.Getwd()
		if err != nil {
			return ansible.CommandSpec{}, err
		}
	}
	return ansible.CommandSpec{Executable: binary, Args: args, Dir: project.WorkDir}, nil
}

// CheckBinary verifies the selected runtime, including configured executables.
func CheckBinary() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := commandSpec(ctx, nil)
	return err
}

func runStdout(args ...string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	spec, err := commandSpec(ctx, args[1:])
	if err != nil {
		return nil, "", err
	}
	return ansible.Observe(ctx, spec, 20*time.Second)
}

// isBenignError reports whether the error (and the stderr text) should be
// treated as "nothing installed" rather than a hard failure.
// ansible-galaxy exits with non-zero codes and prints "None of the provided
// paths were usable" when the roles/collections directory doesn't exist yet.
func isBenignError(err error, stderr string) bool {
	if err == nil {
		return true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		if code == 5 || code == 6 {
			return true
		}
	}
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "none of the provided paths") ||
		strings.Contains(lower, "no roles found") {
		return true
	}
	return false
}

// ListRoles returns installed roles (ansible-galaxy role list).
// Warnings printed to stderr are discarded; only stdout is parsed.
// A benign exit (empty roles path, exit 5/6) returns an empty list, not an error.
func ListRoles() ([]Item, error) {
	out, stderr, err := runStdout("ansible-galaxy", "role", "list")
	if err != nil && !isBenignError(err, stderr) {
		return nil, err
	}
	return parseRoleList(out), nil
}

func parseRoleList(data []byte) []Item {
	var items []Item
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		trimmed := strings.TrimSpace(sc.Text())
		// Skip blank lines and path comment headers ("# /home/…/roles").
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Role entries are formatted as:  "- namespace.role_name, 1.2.3"
		entry := strings.TrimPrefix(trimmed, "- ")
		parts := strings.SplitN(entry, ",", 2)
		item := Item{Name: strings.TrimSpace(parts[0])}
		if len(parts) == 2 {
			item.Version = strings.TrimSpace(parts[1])
		}
		if item.Name != "" {
			items = append(items, item)
		}
	}
	return items
}

// ListCollections returns installed collections (ansible-galaxy collection list).
// Stderr warnings are discarded; only stdout is parsed.
func ListCollections() ([]Item, error) {
	out, stderr, err := runStdout("ansible-galaxy", "collection", "list")
	if err != nil && !isBenignError(err, stderr) {
		return nil, err
	}
	return parseCollectionList(out), nil
}

func parseCollectionList(data []byte) []Item {
	var items []Item
	sc := bufio.NewScanner(bytes.NewReader(data))
	inTable := false
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			inTable = false
			continue
		}
		// Header line starts with "# /path..."
		if strings.HasPrefix(trimmed, "#") {
			inTable = true
			continue
		}
		// Skip dashed separator lines and the "Collection Version" header.
		if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "Collection") {
			continue
		}
		if !inTable {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 {
			items = append(items, Item{Name: fields[0], Version: fields[1]})
		}
	}
	return items
}

// InstallRole installs in the selected project using its active runtime.
func InstallRole(name string) (string, error) { return install("role", name) }

// InstallCollection installs in the selected project using its active runtime.
func InstallCollection(name string) (string, error) { return install("collection", name) }

func install(kind, name string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("a valid %s name is required", kind)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	spec, err := commandSpec(ctx, []string{kind, "install", name})
	if err != nil {
		return "", err
	}
	var output strings.Builder
	_, err = ansible.Execute(ctx, ansible.RunPlan{Command: spec}, func(event ansible.Event) {
		if output.Len() < 4<<20 {
			output.WriteString(event.Line)
			output.WriteByte('\n')
		}
	})
	return output.String(), err
}
