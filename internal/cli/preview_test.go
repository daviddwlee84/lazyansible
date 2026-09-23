package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
)

func fakeListing(t *testing.T, h string) (string, string) {
	t.Helper()
	playbook, marker := fakeAnsible(t, h)
	fixture, err := filepath.Abs("../ansible/testdata/listing-combined.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAZYANSIBLE_CLI_LISTING_FILE", fixture)
	t.Setenv("LAZYANSIBLE_CLI_ARGS", filepath.Join(h, "listing-args"))
	script := `#!/bin/sh
if [ "$1" = --version ]; then echo 'ansible [core 2.21.4]'; exit 0; fi
printf '%s\n' "$@" > "$LAZYANSIBLE_CLI_ARGS"
listing=false
for arg in "$@"; do
  case "$arg" in --list-hosts|--list-tasks|--list-tags) listing=true ;; esac
done
if [ "$listing" != true ]; then printf 'executed\n' > "$LAZYANSIBLE_TEST_MARKER"; exit 99; fi
/bin/cat "$LAZYANSIBLE_CLI_LISTING_FILE"
if [ -n "$LAZYANSIBLE_CLI_WARNING" ]; then printf '%s\n' "$LAZYANSIBLE_CLI_WARNING" >&2; fi
exit "${LAZYANSIBLE_TEST_EXIT:-0}"
`
	if err := os.WriteFile(filepath.Join(h, "bin", "ansible-playbook"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return playbook, marker
}

func TestPreviewCLIUsesSharedScopeAndNeverPrompts(t *testing.T) {
	h := cliHome(t)
	playbook, marker := fakeListing(t, h)
	out, diagnostic, err := command(t, "-C", h, "preview", playbook, "--limit", "localhost", "--tags", "greeting", "-e", "password=PRIVATE_SENTINEL", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var result ansible.ExecutionPreview
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Parsed || len(result.Plays) != 1 || result.Request.Tags != "greeting" || result.Request.Limit != "localhost" || strings.Contains(out, "PRIVATE_SENTINEL") || strings.Contains(diagnostic, "Run this command") {
		t.Fatalf("preview API: %s %s", out, diagnostic)
	}
	args, _ := os.ReadFile(os.Getenv("LAZYANSIBLE_CLI_ARGS"))
	for _, fragment := range []string{"--limit\nlocalhost", "--tags\ngreeting", "-e\npassword=PRIVATE_SENTINEL", "--list-hosts\n--list-tasks\n--list-tags"} {
		if !strings.Contains(string(args), fragment) {
			t.Fatalf("missing scope %q: %s", fragment, args)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("preview executed tasks")
	}
	if _, _, err := command(t, "-C", h, "preview", playbook, "--yes"); err == nil {
		t.Fatal("preview accepted execution-only flag")
	}
}

func TestInspectTagsCLIAndNativeFailureJSON(t *testing.T) {
	h := cliHome(t)
	playbook, marker := fakeListing(t, h)
	tagFile, err := filepath.Abs("../ansible/testdata/listing-tags.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAZYANSIBLE_CLI_LISTING_FILE", tagFile)
	out, _, err := command(t, "-C", h, "inspect", "tags", playbook, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog ansible.TagCatalog
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		t.Fatal(err)
	}
	if !catalog.Parsed || len(catalog.Tags) != 2 {
		t.Fatalf("tag catalogue: %s", out)
	}
	if _, _, err := command(t, "-C", h, "inspect", "tags", playbook, "--tags", "greeting"); err == nil {
		t.Fatal("catalogue accepted a silently ignored UI tag filter")
	}
	t.Setenv("LAZYANSIBLE_TEST_EXIT", "7")
	t.Setenv("LAZYANSIBLE_CLI_WARNING", "password=PRIVATE_FAILURE")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"-C", h, "preview", playbook, "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 7 || !json.Valid(stdout.Bytes()) || !json.Valid(stderr.Bytes()) || strings.Contains(stdout.String()+stderr.String(), "PRIVATE_FAILURE") {
		t.Fatalf("failure output/code: %d %s %s", code, &stdout, &stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("listing failure ran tasks")
	}
}
