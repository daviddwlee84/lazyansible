package ansible

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const previewNotice = "Static Ansible listing; dynamic includes and runtime conditions can change the executed tasks."
const catalogNotice = "Ansible-discovered tags with the UI tag selection omitted; inherited configuration filters and dynamic includes can limit this catalogue."

// listingCommand reuses a prepared installation and project, without another
// resolution or mutation of the original execution plan.
func listingCommand(plan RunPlan, catalog bool) (CommandSpec, RunRequest, error) {
	r := plan.Request
	if r.Kind != "playbook" || r.Playbook == "" || plan.Command.Executable == "" || !filepath.IsAbs(plan.Command.Dir) {
		return CommandSpec{}, r, errors.New("listing requires a prepared playbook plan")
	}
	playbook := r.Playbook
	if !filepath.IsAbs(playbook) {
		playbook = filepath.Join(plan.Command.Dir, playbook)
	}
	if catalog {
		r.Tags = ""
	}
	args := append([]string{playbook}, scopeArgs(r)...)
	if catalog {
		args = append(args, "--list-tags")
	} else {
		args = append(args, "--list-hosts", "--list-tasks", "--list-tags")
	}
	return CommandSpec{Executable: plan.Command.Executable, Dir: plan.Command.Dir, Args: args, Env: append([]string{}, plan.Command.Env...)}, r, nil
}

func observeListing(ctx context.Context, plan RunPlan, catalog bool) (ExecutionPreview, error) {
	p := ExecutionPreview{Request: plan.Request, Runtime: plan.Runtime, ExitCode: -1, Plays: []PreviewPlay{}, Notice: previewNotice}
	spec, request, err := listingCommand(plan, catalog)
	if err != nil {
		return p, err
	}
	p.Request = request
	sanitize := previewSanitizer(request)
	p.Request = safePreviewRequest(request, sanitize)
	p.Command = sanitize(displayCommand(spec))
	out, diagnostic, err := Observe(ctx, spec, 30*time.Second)
	p.ObservedAt = time.Now()
	p.Output = sanitize(string(out))
	p.Diagnostics = sanitize(diagnostic)
	if err == nil {
		p.ExitCode = 0
	} else {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			p.ExitCode = exit.ExitCode()
		}
		// Keep only sanitized diagnostics in the error; retain cancellation
		// identity without wrapping arbitrary external error text.
		if errors.Is(err, context.Canceled) {
			return p, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return p, context.DeadlineExceeded
		}
		message := sanitize(err.Error())
		if p.Diagnostics != "" {
			message += "; " + strings.TrimSpace(p.Diagnostics)
		}
		return p, fmt.Errorf("Ansible listing failed: %s", message)
	}
	p.Plays, p.Parsed = parseListing(p.Output, catalog)
	if !p.Parsed {
		p.Plays = []PreviewPlay{}
		p.Notice += " Output format was not recognized; showing Ansible output without inferred rows."
	}
	return p, nil
}

func safePreviewRequest(r RunRequest, sanitize func(string) string) RunRequest {
	r.Args = ""
	r.ExtraVars = nil
	r.VaultPasswordFile = ""
	r.Env = nil
	for _, value := range []*string{&r.Project.WorkDir, &r.Project.Inventory, &r.Project.PlaybookDir, &r.Project.Executable, &r.Playbook, &r.RolePath, &r.Module, &r.Hosts, &r.Limit, &r.Tags, &r.Executable} {
		*value = sanitize(*value)
	}
	return r
}

// Preview lists the selected playbook's hosts, tasks and tags in one bounded
// invocation. It neither executes tasks nor changes the prepared run plan.
func Preview(ctx context.Context, plan RunPlan) (ExecutionPreview, error) {
	return observeListing(ctx, plan, false)
}

// DiscoverTags omits only the UI's tag selection. It respects all inherited
// Ansible filtering and is not a promise of every tag in dynamic content.
func DiscoverTags(ctx context.Context, plan RunPlan) (TagCatalog, error) {
	p, err := observeListing(ctx, plan, true)
	c := TagCatalog{Request: p.Request, Runtime: p.Runtime, ObservedAt: p.ObservedAt, Command: p.Command, Output: p.Output, Diagnostics: p.Diagnostics, Notice: catalogNotice, Parsed: p.Parsed, ExitCode: p.ExitCode, Tags: []string{}}
	if !p.Parsed && err == nil {
		c.Notice += " Output format was not recognized; showing Ansible output."
	}
	seen := map[string]bool{}
	for _, play := range p.Plays {
		for _, tag := range play.Tags {
			if !seen[tag] {
				seen[tag] = true
				c.Tags = append(c.Tags, tag)
			}
		}
	}
	sort.Strings(c.Tags)
	return c, err
}

var playLine = regexp.MustCompile(`^  play #([0-9]+) \((.*)\): (.*)\tTAGS: \[(.*)\]$`)
var hostLine = regexp.MustCompile(`^    hosts \(([0-9]+)\):$`)

// parseListing recognizes the documented CLI's current text layout, never
// treating unknown/missing sections as an empty successful observation.
func parseListing(output string, catalog bool) ([]PreviewPlay, bool) {
	plays := []PreviewPlay{}
	var current *PreviewPlay
	header := false
	hostsExpected := -1
	section := ""
	patternSeen, tasksSeen, tagsSeen := false, false, false
	complete := func() bool {
		return current != nil && tagsSeen && (catalog || (patternSeen && tasksSeen && hostsExpected == len(current.Hosts) && hostsExpected >= 0))
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "playbook: ") {
			if header || current != nil {
				return nil, false
			}
			header = true
			continue
		}
		if match := playLine.FindStringSubmatch(line); match != nil {
			if !header || (current != nil && !complete()) {
				return nil, false
			}
			number, err := strconv.Atoi(match[1])
			if err != nil || number < 1 {
				return nil, false
			}
			plays = append(plays, PreviewPlay{Number: number, Name: match[3], Hosts: []string{}, Tasks: []PreviewTask{}, Tags: []string{}})
			current = &plays[len(plays)-1]
			hostsExpected = -1
			section = ""
			patternSeen = false
			tasksSeen = false
			tagsSeen = false
			continue
		}
		if current == nil {
			return nil, false
		}
		if strings.HasPrefix(line, "    pattern: ") && !catalog {
			if patternSeen {
				return nil, false
			}
			current.Pattern = strings.TrimPrefix(line, "    pattern: ")
			patternSeen = true
			continue
		}
		if match := hostLine.FindStringSubmatch(line); match != nil && !catalog {
			if hostsExpected >= 0 {
				return nil, false
			}
			var err error
			hostsExpected, err = strconv.Atoi(match[1])
			if err != nil {
				return nil, false
			}
			section = "hosts"
			continue
		}
		if line == "    tasks:" && !catalog {
			if tasksSeen || hostsExpected != len(current.Hosts) {
				return nil, false
			}
			tasksSeen = true
			section = "tasks"
			continue
		}
		if strings.HasPrefix(line, "      TASK TAGS: [") && strings.HasSuffix(line, "]") {
			if tagsSeen || (!catalog && !tasksSeen) {
				return nil, false
			}
			current.Tags = parseTagText(strings.TrimSuffix(strings.TrimPrefix(line, "      TASK TAGS: ["), "]"))
			tagsSeen = true
			section = ""
			continue
		}
		if section == "hosts" && strings.HasPrefix(line, "      ") {
			name := strings.TrimSpace(line)
			if name == "" || len(current.Hosts) >= hostsExpected {
				return nil, false
			}
			current.Hosts = append(current.Hosts, name)
			continue
		}
		if section == "tasks" && strings.HasPrefix(line, "      ") && strings.HasSuffix(line, "]") {
			position := strings.LastIndex(line, "\tTAGS: [")
			if position < 6 {
				return nil, false
			}
			name := strings.TrimSpace(line[6:position])
			if name == "" {
				return nil, false
			}
			current.Tasks = append(current.Tasks, PreviewTask{Name: name, Tags: parseTagText(line[position+len("\tTAGS: [") : len(line)-1])})
			continue
		}
		return nil, false
	}
	return plays, header && complete()
}

func parseTagText(text string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(text, ", ")
}
