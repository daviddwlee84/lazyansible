// Package inventory also handles playbook discovery.
package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daviddwlee84/lazyansible/internal/core"
	"gopkg.in/yaml.v3"
)

// DiscoverPlaybooks walks dir and returns all files that look like Ansible playbooks.
func DiscoverPlaybooks(dir string) ([]*core.Playbook, error) {
	var playbooks []*core.Playbook

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") ||
				name == "roles" || name == "collections" || name == "files" ||
				name == "templates" || name == "vars" || name == "defaults" ||
				name == "handlers" || name == "meta" || name == "tasks" ||
				name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}

		pb, ok := looksLikePlaybook(path)
		if ok {
			playbooks = append(playbooks, pb)
		}
		return nil
	})

	return playbooks, err
}

// ParseSinglePlaybook attempts to parse a single file as an Ansible playbook.
// It is exported so callers (e.g. loadPlaybooksCmd) can probe specific paths.
func ParseSinglePlaybook(path string) (*core.Playbook, bool) {
	return looksLikePlaybook(path)
}

// looksLikePlaybook returns a Playbook if the file is a valid Ansible playbook.
func looksLikePlaybook(path string) (*core.Playbook, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	// Parse as a generic YAML list to detect the playbook shape.
	var raw []interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil || len(raw) == 0 {
		return nil, false
	}

	pb := &core.Playbook{
		Name: displayName(path),
		Path: path,
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err == nil {
		pb.RoleDeclarations = roleDeclarations(path, &document)
	}

	isPlaybook := false
	tagSet := make(map[string]bool)

	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if _, hasHosts := m["hosts"]; hasHosts {
			isPlaybook = true
			if h, ok := m["hosts"].(string); ok {
				pb.Hosts = pbAppendUnique(pb.Hosts, h)
			}
		}
		if _, hasImport := m["import_playbook"]; hasImport {
			isPlaybook = true
		}
		// Collect all tags recursively.
		collectTags(m, tagSet)
	}

	if !isPlaybook {
		return nil, false
	}

	for tag := range tagSet {
		pb.Tags = append(pb.Tags, tag)
	}
	sort.Strings(pb.Tags)

	return pb, true
}

// collectTags recurses through a YAML map/list and collects all "tags" values.
func collectTags(node interface{}, tagSet map[string]bool) {
	switch v := node.(type) {
	case map[string]interface{}:
		if tags, ok := v["tags"]; ok {
			addTags(tags, tagSet)
		}
		// Recurse into task lists.
		for _, key := range []string{"tasks", "pre_tasks", "post_tasks", "block", "rescue", "always", "roles"} {
			if sub, ok := v[key]; ok {
				collectTags(sub, tagSet)
			}
		}
	case []interface{}:
		for _, item := range v {
			collectTags(item, tagSet)
		}
	}
}

func addTags(tags interface{}, tagSet map[string]bool) {
	switch t := tags.(type) {
	case string:
		if t != "" {
			tagSet[t] = true
		}
	case []interface{}:
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				tagSet[s] = true
			}
		}
	}
}

func pbAppendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

func displayName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(strings.TrimSuffix(base, ".yml"), ".yaml")
}

// roleDeclarations observes this file only. In particular, following an import
// or resolving a Jinja expression here would imply an execution graph we do not
// have. yaml.Node supplies source locations without another parser dependency.
func roleDeclarations(path string, doc *yaml.Node) []core.RoleDeclaration {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.SequenceNode {
		return nil
	}
	var result []core.RoleDeclaration
	playIndex := 0
	for _, play := range doc.Content[0].Content {
		if play.Kind != yaml.MappingNode {
			continue
		}
		if imported := yamlValue(play, "import_playbook", "ansible.builtin.import_playbook"); imported != nil {
			result = append(result, newRoleDeclaration(path, imported, "import_playbook", yamlText(imported), "", 0, literalTags(yamlValue(play, "tags")), false, "Imported playbook is not expanded"))
			continue
		}
		if yamlValue(play, "hosts") == nil {
			continue
		}
		playIndex++
		playName := yamlText(yamlValue(play, "name"))
		playTags := literalTags(yamlValue(play, "tags"))
		if declared := yamlValue(play, "roles"); declared != nil {
			if declared.Kind != yaml.SequenceNode {
				result = append(result, newRoleDeclaration(path, declared, "role", yamlText(declared), playName, playIndex, playTags, false, "Role list is not a literal sequence"))
			} else {
				for _, item := range declared.Content {
					name := item
					tags := append([]string{}, playTags...)
					if item.Kind == yaml.MappingNode {
						name = yamlValue(item, "role")
						tags = appendTags(tags, literalTags(yamlValue(item, "tags"))...)
					}
					value := yamlText(name)
					static := name != nil && name.Kind == yaml.ScalarNode && name.Tag == "!!str" && value != "" && !templated(value)
					reason := ""
					if !static {
						reason = "Dynamic or unsupported role name; not resolved"
					}
					if static && item.Kind == yaml.MappingNode && yamlValue(item, "when") != nil {
						reason = "Condition declared; not evaluated"
					}
					result = append(result, newRoleDeclaration(path, item, "role", value, playName, playIndex, tags, static, reason))
				}
			}
		}
		observeTaskReferences(path, play, playName, playIndex, playTags, &result)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Line < result[j].Line })
	// Lines are navigation targets, not selection identity: inserting comments
	// or a different role above the selection must not select another role.
	occurrences := map[string]int{}
	for i := range result {
		ref := &result[i]
		key := fmt.Sprintf("%s|play=%d|%s|%s", path, ref.PlayIndex, ref.Kind, ref.Name)
		occurrences[key]++
		ref.ID = fmt.Sprintf("%s|use=%d", key, occurrences[key])
	}
	return result
}

func newRoleDeclaration(path string, node *yaml.Node, kind, name, playName string, index int, tags []string, static bool, reason string) core.RoleDeclaration {
	if name == "" {
		name = "(unresolved)"
	}
	return core.RoleDeclaration{ID: fmt.Sprintf("%s:%d:%d:%s", path, node.Line, node.Column, kind), Name: name, Kind: kind, PlayName: playName, SourcePath: path, PlayIndex: index, Line: node.Line, Tags: append([]string{}, tags...), Static: static, Reason: reason}
}

func yamlValue(node *yaml.Node, keys ...string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		for _, key := range keys {
			if node.Content[i].Value == key {
				return node.Content[i+1]
			}
		}
	}
	return nil
}
func yamlText(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	return ""
}
func templated(value string) bool {
	return strings.Contains(value, "{{") || strings.Contains(value, "{%")
}
func literalTags(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && node.Value != "" && !templated(node.Value) {
		return []string{node.Value}
	}
	var tags []string
	if node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			tags = appendTags(tags, literalTags(child)...)
		}
	}
	return tags
}
func appendTags(tags []string, add ...string) []string {
	for _, tag := range add {
		tags = pbAppendUnique(tags, tag)
	}
	return tags
}
func observeTaskReferences(path string, node *yaml.Node, playName string, index int, parentTags []string, result *[]core.RoleDeclaration) {
	if node == nil {
		return
	}
	if node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			observeTaskReferences(path, child, playName, index, parentTags, result)
		}
		return
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	tags := appendTags(append([]string{}, parentTags...), literalTags(yamlValue(node, "tags"))...)
	for _, kind := range []string{"import_role", "include_role", "import_tasks", "include_tasks"} {
		if ref := yamlValue(node, kind, "ansible.builtin."+kind); ref != nil {
			name := yamlText(ref)
			if ref.Kind == yaml.MappingNode {
				name = yamlText(yamlValue(ref, "name", "file"))
			}
			*result = append(*result, newRoleDeclaration(path, ref, kind, name, playName, index, tags, false, "Task "+kind+" reference is not expanded"))
		}
	}
	for _, key := range []string{"tasks", "pre_tasks", "post_tasks", "block", "rescue", "always"} {
		observeTaskReferences(path, yamlValue(node, key), playName, index, tags, result)
	}
}
