package ansible

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func listingFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPreviewAndCatalogueKeepPreparedExecutionImmutable(t *testing.T) {
	f := newFixture(t)
	req := RunRequest{Kind: "playbook", Playbook: "site.yml", Project: ProjectContext{WorkDir: f.project, Inventory: "hosts.ini"}, Tags: "greeting", Limit: "localhost", Check: true, Diff: true, Become: true, ExtraVars: []string{"--tags", "password=KNOWN_SECRET"}, VaultPasswordFile: "private.pass", Env: []string{"ANSIBLE_RUN_TAGS=inherited"}}
	plan, err := Prepare(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	originalArgs := append([]string{}, plan.Command.Args...)
	originalEnv := append([]string{}, plan.Command.Env...)
	t.Setenv("LAZYANSIBLE_LISTING_OUTPUT", listingFixture(t, "listing-combined.txt"))
	preview, err := Preview(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Parsed || len(preview.Plays) != 1 || len(preview.Plays[0].Tasks) != 2 || preview.ExitCode != 0 {
		t.Fatalf("preview result: %+v", preview)
	}
	calls := f.calls(t)
	call := calls[len(calls)-1]
	if call.Dir != canonical(f.project) || !reflect.DeepEqual(call.Args, append(append([]string{}, originalArgs...), "--list-hosts", "--list-tasks", "--list-tags")) {
		t.Fatalf("preview lost run scope: %+v", call)
	}
	t.Setenv("LAZYANSIBLE_LISTING_OUTPUT", listingFixture(t, "listing-tags.txt"))
	catalog, err := DiscoverTags(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.Parsed || !reflect.DeepEqual(catalog.Tags, []string{"greeting", "summary"}) || catalog.Request.Tags != "" {
		t.Fatalf("catalogue: %+v", catalog)
	}
	calls = f.calls(t)
	call = calls[len(calls)-1]
	if strings.Contains(strings.Join(call.Args, " "), "--tags greeting") {
		t.Fatal("catalogue retained UI tag selection")
	}
	joined := strings.Join(call.Args, "|")
	if !strings.Contains(joined, "-e|--tags|-e|password=KNOWN_SECRET") || !strings.HasSuffix(joined, "--list-tags") {
		t.Fatalf("flag-looking variable altered: %q", call.Args)
	}
	if !reflect.DeepEqual(originalArgs, plan.Command.Args) || !reflect.DeepEqual(originalEnv, plan.Command.Env) || plan.Request.Tags != "greeting" {
		t.Fatal("listing mutated execution plan")
	}
	for _, result := range []any{preview, catalog} {
		data, _ := json.Marshal(result)
		if strings.Contains(string(data), "KNOWN_SECRET") || strings.Contains(string(data), "private.pass") {
			t.Fatalf("observation JSON leaked inputs: %s", data)
		}
	}
}

func TestListingParserPreservesOrderAndEmptyVersusUnknown(t *testing.T) {
	base := listingFixture(t, "listing-combined.txt")
	second := "\n  play #2 (local): 中文 play\tTAGS: []\n    pattern: ['local']\n    hosts (1):\n      localhost\n    tasks:\n      duplicate\tTAGS: [a]\n      duplicate\tTAGS: [b]\n      TASK TAGS: [a, b]\n"
	plays, ok := parseListing(base+second, false)
	if !ok || len(plays) != 2 || len(plays[1].Tasks) != 2 || plays[1].Tasks[1].Tags[0] != "b" {
		t.Fatalf("order/duplicates lost: %+v %v", plays, ok)
	}
	empty := "playbook: site.yml\n  play #1 (local): local\tTAGS: []\n    pattern: ['local']\n    hosts (1):\n      localhost\n    tasks:\n      TASK TAGS: []\n"
	plays, ok = parseListing(empty, false)
	if !ok || len(plays[0].Tasks) != 0 || len(plays[0].Hosts) != 1 {
		t.Fatalf("valid zero tasks misclassified: %+v %v", plays, ok)
	}
	for _, input := range []string{"", base + "unknown substantive output\n", strings.Replace(base, "hosts (1)", "hosts (2)", 1), strings.Replace(base, "    tasks:\n", "", 1)} {
		if _, ok := parseListing(input, false); ok {
			t.Fatalf("unknown/incomplete format was trusted: %q", input)
		}
	}
	dynamic := strings.Replace(base, "demo : Print the selected message", "include_tasks", 1)
	plays, ok = parseListing(dynamic, false)
	if !ok || len(plays[0].Tasks) != 2 || plays[0].Tasks[0].Name != "include_tasks" {
		t.Fatal("dynamic include row was expanded or removed")
	}
}

func TestPreviewFallbackFailureAndKnownSecretSanitization(t *testing.T) {
	f := newFixture(t)
	plan, err := Prepare(context.Background(), RunRequest{Kind: "playbook", Playbook: "site.yml", Project: ProjectContext{WorkDir: f.project}, ExtraVars: []string{`{"password":"literal secret","normal":true}`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAZYANSIBLE_LISTING_OUTPUT", "\x1b]52;c;YWJj\aunrecognized format: literal secret\r\n")
	t.Setenv("LAZYANSIBLE_LISTING_DIAGNOSTIC", `password="diagnostic secret" https://user:urlsecret@example.invalid/?token=querysecret`)
	result, err := Preview(context.Background(), plan)
	if err != nil || result.Parsed || result.ExitCode != 0 || !strings.Contains(result.Output, "unrecognized format") {
		t.Fatalf("fallback changed native success: %+v %v", result, err)
	}
	data, _ := json.Marshal(result)
	for _, secret := range []string{"literal secret", "diagnostic secret", "urlsecret", "querysecret", "\\u001b"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("fallback exposed sensitive/control content: %s", data)
		}
	}
	t.Setenv("LAZYANSIBLE_LISTING_FAILED", "1")
	result, err = Preview(context.Background(), plan)
	if err == nil || result.ExitCode != 7 || result.Parsed || strings.Contains(err.Error(), "diagnostic secret") {
		t.Fatalf("native failure lost or exposed secrets: %+v %v", result, err)
	}
}

func TestPreviewCancellationCapsAndKindGuard(t *testing.T) {
	f := newFixture(t)
	plan, err := Prepare(context.Background(), RunRequest{Kind: "playbook", Playbook: "site.yml", Project: ProjectContext{WorkDir: f.project}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAZYANSIBLE_LISTING_SLEEP", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = Preview(ctx, plan)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("listing not cancelled: %v", err)
	}
	t.Setenv("LAZYANSIBLE_LISTING_SLEEP", "")
	t.Setenv("LAZYANSIBLE_LISTING_OVERSIZE", "1")
	result, err := Preview(context.Background(), plan)
	if err == nil || len(result.Output) > maxProbeOutput || result.Parsed {
		t.Fatalf("oversized observation accepted: %d %v", len(result.Output), err)
	}
	plan.Request.Kind = "role"
	before := len(f.calls(t))
	if _, err := Preview(context.Background(), plan); err == nil {
		t.Fatal("role plan accepted")
	}
	if len(f.calls(t)) != before {
		t.Fatal("unsupported plan spawned a process")
	}
}
