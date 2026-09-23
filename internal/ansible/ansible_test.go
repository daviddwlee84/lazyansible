package ansible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordedCall struct {
	Name                              string
	Args                              []string
	Dir, Callback, Color, RoleContent string
}

// Fake executables delegate to this process; no installed Ansible, inventory,
// package registry, or network is used by these tests.
func TestMain(m *testing.M) {
	if name := os.Getenv("LAZYANSIBLE_FAKE_COMMAND"); name != "" {
		os.Exit(fakeCommand(name))
	}
	os.Exit(m.Run())
}

func fakeCommand(name string) int {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	dir, _ := os.Getwd()
	call := recordedCall{Name: name, Args: args, Dir: dir, Callback: os.Getenv("ANSIBLE_STDOUT_CALLBACK"), Color: os.Getenv("ANSIBLE_FORCE_COLOR")}
	if name == "ansible-playbook" && len(args) > 0 && strings.Contains(filepath.Base(args[0]), "lazyansible-role-") {
		data, _ := os.ReadFile(args[0])
		call.RoleContent = string(data)
	}
	if file := os.Getenv("LAZYANSIBLE_TEST_LOG"); file != "" {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			data, _ := json.Marshal(call)
			data = append(data, '\n')
			_, _ = f.Write(data)
			_ = f.Close()
		}
	}
	contains := func(value string) bool {
		for _, arg := range args {
			if arg == value {
				return true
			}
		}
		return false
	}
	version := os.Getenv("LAZYANSIBLE_TEST_VERSION")
	if version == "" {
		version = "2.20.5"
	}
	if name == "uv" {
		if contains("--version") {
			fmt.Println("uv 0.11.13")
			return 0
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "list" {
			if os.Getenv("LAZYANSIBLE_BAD_TOOLS") != "" {
				fmt.Println("changed unsupported format")
				return 0
			}
			constraint := os.Getenv("LAZYANSIBLE_TEST_CONSTRAINT")
			if constraint == "" {
				constraint = ">=2.20,<2.21"
			}
			fmt.Printf("ansible-core v%s [required: %s] [CPython 3.13.3] (%s)\n- ansible (%s)\n", version, constraint, os.Getenv("LAZYANSIBLE_TEST_TOOL_DIR"), os.Getenv("LAZYANSIBLE_TEST_TOOL_DIR")+"/bin/ansible")
			return 0
		}
		if len(args) >= 2 && args[0] == "pip" && args[1] == "list" {
			if contains("--outdated") {
				if os.Getenv("LAZYANSIBLE_FAILED_UPDATE") != "" {
					fmt.Fprintln(os.Stderr, "registry unavailable https://user:supersecret@private.invalid/simple/?token=topsecret")
					return 2
				}
				if os.Getenv("LAZYANSIBLE_BAD_JSON") != "" {
					fmt.Println("not json")
					return 0
				}
				fmt.Println(`[{"name":"ansible-core","version":"2.20.5","latest_version":"2.21.4","latest_filetype":"wheel"}]`)
				return 0
			}
			fmt.Println(`[{"name":"ansible-core","version":"2.20.5"},{"name":"jinja2","version":"3.1.6"}]`)
			return 0
		}
		fmt.Println("operation completed")
		return 0
	}
	if contains("--version") {
		fmt.Printf("ansible [core %s]\n  python version = 3.13.3 (test) (%s/bin/python3)\n", version, os.Getenv("LAZYANSIBLE_TEST_TOOL_DIR"))
		return 0
	}
	if name == "ansible-inventory" {
		if data := os.Getenv("LAZYANSIBLE_TEST_INVENTORY_JSON"); data != "" {
			fmt.Println(data)
			return 0
		}
		if os.Getenv("LAZYANSIBLE_BAD_JSON") != "" {
			fmt.Println("broken")
			return 0
		}
		fmt.Fprintln(os.Stderr, "[WARNING]: inventory warning")
		fmt.Println(`{"all":{"children":["web"]},"web":{"hosts":["beta","alpha"],"vars":{"enabled":true}},"_meta":{"hostvars":{"alpha":{"ansible_host":"127.0.0.1","nested":{"value":42}},"beta":{"enabled":false}}}}`)
		return 0
	}
	if name == "ansible-config" {
		if data := os.Getenv("LAZYANSIBLE_TEST_CONFIG_JSON"); data != "" {
			fmt.Println(data)
			return 0
		}
		fmt.Println(`[{"name":"DEFAULT_STDOUT_CALLBACK","value":"clean","origin":"project/ansible.cfg","type":"string"}]`)
		return 0
	}
	if os.Getenv("LAZYANSIBLE_SLEEP") != "" {
		fmt.Println("ready")
		time.Sleep(time.Minute)
		return 0
	}
	if os.Getenv("LAZYANSIBLE_LONG_LINE") != "" {
		fmt.Println(strings.Repeat("x", 200000))
	}
	if os.Getenv("LAZYANSIBLE_TEST_ANSI") != "" {
		fmt.Print("\x1b]52;c;c2VjcmV0\a\x1b[31mok: [localhost]\x1b[0m\n")
		fmt.Fprint(os.Stderr, "\x1b]0;untrusted title\a\x1b[2Jdiagnostic\n")
		return 0
	}
	fmt.Println("ok: [localhost]")
	fmt.Fprintln(os.Stderr, "warning output")
	if os.Getenv("LAZYANSIBLE_EXIT_FAIL") != "" {
		return 3
	}
	return 0
}

type fixture struct {
	bin, tool, project, log, cache string
	opts                           RuntimeOptions
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := canonical(t.TempDir())
	f := fixture{bin: filepath.Join(root, "bin"), tool: filepath.Join(root, "uv", "ansible-core"), project: filepath.Join(root, "project with spaces"), log: filepath.Join(root, "calls.jsonl"), cache: filepath.Join(root, "cache")}
	for _, dir := range []string{f.bin, filepath.Join(f.tool, "bin"), f.project} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeCommand := func(path, name string) {
		data := "#!/bin/sh\nLAZYANSIBLE_FAKE_COMMAND=" + name + " exec '" + strings.ReplaceAll(testBinary, "'", `'"'"'`) + "' -- \"$@\"\n"
		if err := os.WriteFile(path, []byte(data), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"ansible", "ansible-playbook", "ansible-inventory", "ansible-config", "ansible-galaxy", "python3"} {
		path := filepath.Join(f.tool, "bin", name)
		writeCommand(path, name)
		if err := os.Symlink(path, filepath.Join(f.bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	writeCommand(filepath.Join(f.bin, "uv"), "uv")
	if err := os.WriteFile(filepath.Join(f.project, "site.yml"), []byte("- hosts: all\n  tasks: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	receipt := `[tool]
requirements = [{ name = "ansible-core" }]
[tool.options]
index-url = "https://private.invalid/simple"
index-strategy = "unsafe-first-match"
[[tool.options.index]]
url = "https://mirror.invalid/simple"
default = false
explicit = false
format = "simple"
authenticate = "auto"
`
	if err := os.WriteFile(filepath.Join(f.tool, "uv-receipt.toml"), []byte(receipt), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", f.bin)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("LAZYANSIBLE_TEST_TOOL_DIR", f.tool)
	t.Setenv("LAZYANSIBLE_TEST_LOG", f.log)
	for _, key := range []string{"UV_INDEX", "UV_DEFAULT_INDEX", "UV_INDEX_URL", "UV_EXTRA_INDEX_URL", "UV_INDEX_STRATEGY", "UV_CONFIG_FILE", "UV_KEYRING_PROVIDER", "UV_EXCLUDE_NEWER"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	f.opts = RuntimeOptions{CacheDir: f.cache, WorkDir: f.project}
	return f
}
func (f fixture) calls(t *testing.T) []recordedCall {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil {
		t.Fatal(err)
	}
	var calls []recordedCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call recordedCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func TestPrepareExecutePreservesContextAndRedactsReview(t *testing.T) {
	f := newFixture(t)
	t.Setenv("ANSIBLE_STDOUT_CALLBACK", "clean")
	secret := "password=not-for-review"
	request := RunRequest{Kind: "playbook", Project: ProjectContext{WorkDir: f.project, Inventory: "inventory.ini"}, Playbook: "site.yml", Check: true, Diff: true, ExtraVars: []string{secret, "name=a b; $(touch nope)"}, VaultPasswordFile: "/private/vault.pass"}
	plan, err := Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Command.Dir != f.project || plan.Runtime.CoreVersion != "2.20.5" || plan.Callback != "default" {
		t.Fatalf("wrong plan context: %+v", plan)
	}
	encoded, _ := json.Marshal(plan)
	if strings.Contains(string(encoded), secret) || strings.Contains(plan.Preview, "vault.pass") {
		t.Fatalf("review leaked sensitive arguments: %s", encoded)
	}
	var events []Event
	result, err := Execute(context.Background(), plan, func(e Event) { events = append(events, e) })
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("execute: %+v %v", result, err)
	}
	if len(events) != 2 {
		t.Fatalf("missing stdout/stderr events: %+v", events)
	}
	calls := f.calls(t)
	call := calls[len(calls)-1]
	if call.Dir != f.project || call.Callback != "default" || call.Color != "0" {
		t.Fatalf("wrong child context: %+v", call)
	}
	if !reflect.DeepEqual(call.Args, plan.Command.Args) {
		t.Fatalf("execution changed reviewed args: %q vs %q", call.Args, plan.Command.Args)
	}
	if os.Getenv("ANSIBLE_STDOUT_CALLBACK") != "clean" {
		t.Fatal("parent environment was modified")
	}
}

func TestSameInstallationDespiteShadowedPath(t *testing.T) {
	f := newFixture(t)
	shadow := t.TempDir()
	if err := os.WriteFile(filepath.Join(shadow, "ansible"), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shadow+string(os.PathListSeparator)+f.bin)
	plan, err := Prepare(context.Background(), RunRequest{Kind: "adhoc", Module: "ping", Project: ProjectContext{WorkDir: f.project}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Command.Executable != filepath.Join(f.tool, "bin", "ansible") || !plan.Runtime.Managed {
		t.Fatalf("mixed installation selected: %+v", plan)
	}
}

func TestRolePlanCreatesTemporaryInputOnlyAtExecution(t *testing.T) {
	f := newFixture(t)
	role := filepath.Join(f.project, "roles", "a: quoted role")
	if err := os.MkdirAll(role, 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := Prepare(context.Background(), RunRequest{Kind: "role", RolePath: role, Project: ProjectContext{WorkDir: f.project}, Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Command.Args[0] != "<generated-role-playbook>" {
		t.Fatal("role plan was materialized early")
	}
	if _, err := Execute(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}
	calls := f.calls(t)
	call := calls[len(calls)-1]
	if !strings.Contains(call.RoleContent, role) {
		t.Fatalf("role path was lost: %s", call.RoleContent)
	}
	if _, err := os.Stat(call.Args[0]); !os.IsNotExist(err) {
		t.Fatalf("temporary playbook not removed: %v", err)
	}
}

func TestExecuteFailureLongLinesAndCancellation(t *testing.T) {
	f := newFixture(t)
	plan := RunPlan{Command: CommandSpec{Executable: filepath.Join(f.tool, "bin", "ansible"), Dir: f.project}}
	t.Setenv("LAZYANSIBLE_LONG_LINE", "1")
	t.Setenv("LAZYANSIBLE_EXIT_FAIL", "1")
	maxLine := 0
	result, err := Execute(context.Background(), plan, func(e Event) {
		if len(e.Line) > maxLine {
			maxLine = len(e.Line)
		}
	})
	if err == nil || result.ExitCode != 3 || maxLine != 200000 {
		t.Fatalf("failure/long-line handling: %+v %v %d", result, err, maxLine)
	}
	t.Setenv("LAZYANSIBLE_SLEEP", "1")
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	_, err = Execute(ctx, plan, func(e Event) {
		if e.Line == "ready" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || time.Since(start) > 3*time.Second {
		t.Fatalf("cancellation failed: %v", err)
	}
}

func TestInspectTypedJSONAndErrors(t *testing.T) {
	f := newFixture(t)
	project := ProjectContext{WorkDir: f.project, Inventory: "hosts.yml", PlaybookDir: "playbooks"}
	inventory, err := InspectInventory(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Hosts) != 2 || inventory.Hosts[0].Name != "alpha" || inventory.Hosts[0].Vars["nested"] == nil || !strings.Contains(inventory.Warnings, "WARNING") {
		t.Fatalf("inventory snapshot: %+v", inventory)
	}
	if value := inventory.Hosts[0].Vars["nested"].(map[string]any)["value"]; value != json.Number("42") {
		t.Fatalf("inventory number lost its JSON type: %#v", value)
	}
	config, err := InspectConfig(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Entries) != 1 || config.Entries[0].Value != "clean" {
		t.Fatalf("config snapshot: %+v", config)
	}
	t.Setenv("LAZYANSIBLE_BAD_JSON", "1")
	if _, err := InspectInventory(context.Background(), project); err == nil {
		t.Fatal("invalid inventory JSON was accepted")
	}
}

func TestScopedUpdatesCacheAndFailedRefresh(t *testing.T) {
	f := newFixture(t)
	status, err := Check(context.Background(), f.opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "available" || status.LatestVersion != "2.21.4" || status.Runtime.Constraint != ">=2.20,<2.21" {
		t.Fatalf("newer release observation: %+v", status)
	}
	calls := f.calls(t)
	var query recordedCall
	for _, call := range calls {
		if strings.Contains(strings.Join(call.Args, " "), "--outdated") {
			query = call
		}
	}
	args := strings.Join(query.Args, " ")
	if !strings.Contains(args, "--exclude jinja2") || !strings.Contains(args, "--default-index https://private.invalid/simple") || !strings.Contains(args, "--index https://mirror.invalid/simple") || query.Dir != f.tool {
		t.Fatalf("update query changed scope/source: %+v", query)
	}
	count := len(calls)
	cached, err := Check(context.Background(), f.opts, false)
	if err != nil || !cached.Cached {
		t.Fatalf("cache not used: %+v %v", cached, err)
	}
	for _, call := range f.calls(t)[count:] {
		if strings.Contains(strings.Join(call.Args, " "), "--outdated") {
			t.Fatal("fresh cache performed a network check")
		}
	}
	t.Setenv("LAZYANSIBLE_FAILED_UPDATE", "1")
	failed, err := Check(context.Background(), f.opts, true)
	if err == nil || failed.State != "unknown" || !failed.Stale || failed.LatestVersion != "2.21.4" || strings.Contains(failed.Error, "supersecret") || strings.Contains(failed.Error, "topsecret") {
		t.Fatalf("failed check lost prior result or exposed credentials: %+v %v", failed, err)
	}
	cached, err = Check(context.Background(), f.opts, false)
	if err == nil || !cached.Cached {
		t.Fatalf("failed attempt not cached: %+v %v", cached, err)
	}
}

func TestUpgradePreservesOwnerAndRejectsDrift(t *testing.T) {
	f := newFixture(t)
	plan, err := PrepareUpgrade(context.Background(), f.opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Command.Args, []string{"tool", "upgrade", "ansible-core"}) {
		t.Fatalf("upgrade changed owner/constraints: %q", plan.Command.Args)
	}
	t.Setenv("LAZYANSIBLE_TEST_VERSION", "2.21.4")
	if _, err := Execute(context.Background(), plan, nil); err == nil || !strings.Contains(err.Error(), "changed after review") {
		t.Fatalf("owner/version drift accepted: %v", err)
	}
	for _, call := range f.calls(t) {
		if len(call.Args) > 1 && call.Args[0] == "tool" && call.Args[1] == "upgrade" {
			t.Fatal("upgrade ran after drift")
		}
	}
}

func TestUnsupportedReceiptAndUVNeverFallback(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.tool, "uv-receipt.toml"), []byte("[tool.options]\nprerelease = 'allow'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := Check(context.Background(), f.opts, false)
	if err == nil || status.State != "unknown" {
		t.Fatalf("unsupported receipt accepted: %+v %v", status, err)
	}
	for _, call := range f.calls(t) {
		if len(call.Args) > 0 && call.Args[0] == "pip" {
			t.Fatal("unsupported receipt queried a fallback source")
		}
	}
	t.Setenv("LAZYANSIBLE_BAD_TOOLS", "1")
	status, err = Check(context.Background(), f.opts, true)
	if err == nil || status.State != "unknown" || status.Runtime.Managed {
		t.Fatalf("unsupported uv ownership was trusted: %+v %v", status, err)
	}
}

func TestInstallCommunityIncludesCoreCommandsWithoutForce(t *testing.T) {
	f := newFixture(t)
	plan, err := PrepareInstall(context.Background(), f.opts, "ansible==13.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Command.Args, []string{"tool", "install", "ansible==13.0.0", "--with-executables-from", "ansible-core"}) {
		t.Fatalf("community entrypoints missing: %q", plan.Command.Args)
	}
	if _, err := PrepareInstall(context.Background(), f.opts, "evil @ https://example.invalid/pkg.whl"); err == nil {
		t.Fatal("unrecognized package source accepted")
	}
}

func TestMissingHomeNeverCreatesRelativeUpdateCache(t *testing.T) {
	f := newFixture(t)
	t.Chdir(f.project)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	opts := f.opts
	opts.CacheDir = ""
	status, err := Check(context.Background(), opts, false)
	if err == nil || status.State != "unknown" || !strings.Contains(err.Error(), "cache directory") {
		t.Fatalf("missing home accepted: %+v %v", status, err)
	}
	if _, err := os.Stat(filepath.Join(f.project, ".cache")); !os.IsNotExist(err) {
		t.Fatal("created a cache relative to the project")
	}
	if _, err := cacheLocation(RuntimeOptions{CacheDir: "relative"}, strings.Repeat("a", 64)); err == nil {
		t.Fatal("explicit relative cache accepted")
	}
}

func TestInstallVerifiesSelectedOwnerWithoutSwitchingIt(t *testing.T) {
	f := newFixture(t)
	plan, err := PrepareInstall(context.Background(), f.opts, "ansible-core")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), plan, nil); err != nil {
		t.Fatalf("existing shared uv owner not verified: %v", err)
	}
	external := filepath.Join(t.TempDir(), "ansible")
	script, err := os.ReadFile(filepath.Join(f.tool, "bin", "ansible"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(external, script, 0700); err != nil {
		t.Fatal(err)
	}
	opts := f.opts
	opts.Executable = external
	plan, err = PrepareInstall(context.Background(), opts, "ansible-core")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Runtime.Managed || len(plan.Notes) == 0 {
		t.Fatal("external owner was omitted from the installation review")
	}
	result, err := Execute(context.Background(), plan, nil)
	if err == nil || result.ExitCode != 0 || !strings.Contains(err.Error(), "uv completed installation") {
		t.Fatalf("shadowed installation claimed active: %+v %v", result, err)
	}
	for _, call := range f.calls(t) {
		for _, arg := range call.Args {
			if arg == "--force" {
				t.Fatal("installation forced takeover")
			}
		}
	}
}

func TestRuntimeAndCacheNeverExposeSourceCredentials(t *testing.T) {
	f := newFixture(t)
	t.Setenv("LAZYANSIBLE_TEST_CONSTRAINT", "git+https://user:supersecret@private.invalid/repo?token=topsecret")
	status, err := Check(context.Background(), f.opts, false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(status)
	if strings.Contains(string(data), "supersecret") || strings.Contains(string(data), "topsecret") {
		t.Fatalf("runtime result exposed source credentials: %s", data)
	}
	files, err := os.ReadDir(f.cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(f.cache, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "supersecret") || strings.Contains(string(data), "topsecret") {
			t.Fatal("cached source credentials")
		}
	}
}

func TestInspectorPrecisionRedactionAndScalarTypes(t *testing.T) {
	f := newFixture(t)
	t.Setenv("LAZYANSIBLE_TEST_INVENTORY_JSON", `{"all":{"hosts":["alpha"],"vars":{"group_password":"group-credential","count":9223372036854775807}},"_meta":{"hostvars":{"alpha":{"large":9007199254740993,"nullable":null,"enabled":true,"nested":{"api_key":"api-credential","array":[false,null,9007199254740995,{"private_key":"private-credential"}],"endpoint":"https://user:url-credential@example.invalid/path?token=query-credential"}}}}}`)
	t.Setenv("LAZYANSIBLE_TEST_CONFIG_JSON", `[{"name":"DEFAULT_VAULT_PASSWORD_FILE","value":"private-password-path","origin":"project/ansible.cfg"},{"name":"ORDINARY","value":{"large":9007199254740997,"secret":"config-credential","items":[true,null,42]},"origin":"https://user:origin-credential@example.invalid/config?token=origin-query"}]`)
	project := ProjectContext{WorkDir: f.project}
	inventory, err := InspectInventory(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	vars := inventory.Hosts[0].Vars
	if vars["large"] != json.Number("9007199254740993") || vars["enabled"] != true || vars["nullable"] != nil {
		t.Fatalf("scalar values changed: %#v", vars)
	}
	array := vars["nested"].(map[string]any)["array"].([]any)
	if len(array) != 4 || array[0] != false || array[1] != nil || array[2] != json.Number("9007199254740995") {
		t.Fatalf("array scalar values changed: %#v", array)
	}
	if inventory.Groups[0].Vars["count"] != json.Number("9223372036854775807") {
		t.Fatal("group integer lost precision")
	}
	config, err := InspectConfig(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if config.Entries[0].Value != "[redacted]" {
		t.Fatal("sensitive config name did not mask its value")
	}
	ordinary := config.Entries[1].Value.(map[string]any)
	if ordinary["large"] != json.Number("9007199254740997") {
		t.Fatal("configuration integer lost precision")
	}
	data, err := json.Marshal(struct {
		Inventory InventorySnapshot
		Config    ConfigSnapshot
	}{inventory, config})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"group-credential", "api-credential", "private-credential", "url-credential", "query-credential", "private-password-path", "config-credential", "origin-credential", "origin-query"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("inspector JSON retained secret %q", secret)
		}
	}
	for _, number := range []string{"9007199254740993", "9007199254740995", "9007199254740997", "9223372036854775807"} {
		if !strings.Contains(string(data), number) {
			t.Fatalf("roundtrip lost number %s", number)
		}
	}
}

func TestExternalEventsStripTerminalControlSequences(t *testing.T) {
	f := newFixture(t)
	t.Setenv("LAZYANSIBLE_TEST_ANSI", "1")
	var events []Event
	_, err := Execute(context.Background(), RunPlan{Command: CommandSpec{Executable: filepath.Join(f.tool, "bin", "ansible"), Dir: f.project}}, func(event Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]bool{}
	for _, event := range events {
		if strings.ContainsAny(event.Line, "\x1b\a") {
			t.Fatalf("terminal controls reached events: %q", event.Line)
		}
		lines[event.Line] = true
	}
	if !lines["ok: [localhost]"] || !lines["diagnostic"] {
		t.Fatalf("stripping changed visible output: %#v", events)
	}
}
