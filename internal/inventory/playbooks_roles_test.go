package inventory

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRoleDeclarationsKeepLocationsRepeatedUsesAndUnknownReferences(t *testing.T) {
	body := `---
- import_playbook: unavailable.yml
- name: First play
  hosts: all
  tags: [whole_play, "{{ dynamic_tag }}"]
  pre_tasks:
    - ansible.builtin.include_role:
        name: "{{ selected_role }}"
  roles:
    - demo
    - role: demo
      tags: [greeting, whole_play]
      when: ready
    - role: "{{ selected_role }}"
      tags: [conditional]
    - acme.example.demo
  tasks:
    - import_tasks: unavailable-tasks.yml
    - block:
        - ansible.builtin.import_role:
            name: imported_demo
      tags: [nested]
- name: Second play
  hosts: local
  roles: [demo, demo]
`
	path := filepath.Join(t.TempDir(), "site.yml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	pb, ok := ParseSinglePlaybook(path)
	if !ok {
		t.Fatal("valid playbook not discovered")
	}
	refs := pb.RoleDeclarations
	if len(refs) != 10 {
		t.Fatalf("declarations=%d %+v", len(refs), refs)
	}
	if refs[0].Kind != "import_playbook" || refs[0].Static || refs[0].PlayIndex != 0 {
		t.Fatalf("import expanded/invented: %+v", refs[0])
	}
	ids := map[string]bool{}
	var literalDemo int
	lastLine := 0
	for _, ref := range refs {
		if ids[ref.ID] {
			t.Fatalf("declarations collapsed identity: %s", ref.ID)
		}
		ids[ref.ID] = true
		if ref.SourcePath != path || ref.Line < lastLine {
			t.Fatalf("bad source order/location: %+v", ref)
		}
		lastLine = ref.Line
		if ref.Name == "demo" && ref.Static {
			literalDemo++
		}
		for _, tag := range ref.Tags {
			if strings.Contains(tag, "{{") {
				t.Fatalf("dynamic tag treated as literal: %q", tag)
			}
		}
		if ref.Kind == "include_role" || ref.Kind == "import_role" || ref.Kind == "import_tasks" {
			if ref.Static || ref.Reason == "" {
				t.Fatalf("unknown reference evaluated: %+v", ref)
			}
		}
	}
	if literalDemo != 4 {
		t.Fatalf("repeated roles lost: %d", literalDemo)
	}
	tagged := refs[3]
	if tagged.Name != "demo" || !tagged.Static || tagged.PlayIndex != 1 || tagged.PlayName != "First play" || !reflect.DeepEqual(tagged.Tags, []string{"whole_play", "greeting"}) {
		t.Fatalf("declaration context: %+v", tagged)
	}
	if tagged.Line != strings.Count(body[:strings.Index(body, "    - role: demo")], "\n")+1 {
		t.Fatalf("line=%d", tagged.Line)
	}
	if !strings.Contains(tagged.Reason, "not evaluated") {
		t.Fatalf("when not qualified: %+v", tagged)
	}
	if refs[8].PlayIndex != 2 || refs[9].PlayIndex != 2 || refs[8].ID == refs[9].ID {
		t.Fatal("same-line repeated roles need distinct identity")
	}
}

func TestRoleAliasAndDynamicListStayUnknown(t *testing.T) {
	for _, body := range []string{
		"- hosts: all\n  roles: '{{ roles_for_host }}'\n",
		"- hosts: all\n  vars:\n    shared: &shared [demo]\n  roles: *shared\n",
	} {
		path := filepath.Join(t.TempDir(), "site.yml")
		os.WriteFile(path, []byte(body), 0600)
		pb, ok := ParseSinglePlaybook(path)
		if !ok || len(pb.RoleDeclarations) != 1 || pb.RoleDeclarations[0].Static || pb.RoleDeclarations[0].Reason == "" {
			t.Fatalf("expanded dynamic/alias declaration: %+v", pb)
		}
	}
}
