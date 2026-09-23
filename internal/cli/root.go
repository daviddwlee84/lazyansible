// Package cli exposes the same Ansible services used by the dashboard.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/buildinfo"
	"github.com/daviddwlee84/lazyansible/internal/config"
	"github.com/daviddwlee84/lazyansible/internal/paths"
	"github.com/daviddwlee84/lazyansible/internal/ui"
)

// Options supplies terminal streams and permits deterministic entry-policy tests.
type Options struct {
	In         io.Reader
	Out        io.Writer
	Err        io.Writer
	IsTerminal func() bool
	Dashboard  func(ui.Config, config.Config) error
}

type flags struct {
	config, workdir, inventory, playbookDir                      string
	noMouse, notify, check, diff, checkUpdates, json, initConfig bool
}

type effective struct {
	Path    string        `json:"path" yaml:"path"`
	Legacy  bool          `json:"legacy" yaml:"legacy"`
	WorkDir string        `json:"workdir" yaml:"workdir"`
	Config  config.Config `json:"config" yaml:"config"`
}

// ExitError carries a child or usage status without calling os.Exit in handlers.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string           { return e.Err.Error() }
func (e *ExitError) Unwrap() error           { return e.Err }
func usage(format string, args ...any) error { return &ExitError{2, fmt.Errorf(format, args...)} }

func NewRootCommand(options Options) *cobra.Command {
	if options.In == nil {
		options.In = os.Stdin
	}
	if options.Out == nil {
		options.Out = os.Stdout
	}
	if options.Err == nil {
		options.Err = os.Stderr
	}
	if options.IsTerminal == nil {
		options.IsTerminal = func() bool { return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) }
	}
	if options.Dashboard == nil {
		options.Dashboard = dashboard
	}
	f := &flags{}
	root := &cobra.Command{
		Use: "lazyansible", Short: "Inspect and run Ansible with a keyboard-driven dashboard",
		Long:          "A personal lazyansible fork. Run without a subcommand in a terminal to open the dashboard.\nAll operations keep an explicit project working directory; Ansible is never installed implicitly.",
		SilenceErrors: true, SilenceUsage: true, Version: buildinfo.String(),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.initConfig {
				if f.json {
					return usage("--init-config does not support --json")
				}
				return initConfig(cmd, f)
			}
			if f.json {
				return usage("select a read command with --json, for example: lazyansible runtime status --json")
			}
			if !options.IsTerminal() {
				return usage("dashboard needs input/output terminals; use 'lazyansible --help' or 'lazyansible inspect inventory --json'")
			}
			e, err := loadEffective(cmd, f)
			if err != nil {
				return err
			}
			return options.Dashboard(ui.Config{Context: cmd.Context(), InventoryPath: e.Config.Inventory, PlaybookDir: e.Config.PlaybookDir, WorkDir: e.WorkDir, DefaultCheckMode: e.Config.DefaultCheckMode, DefaultDiffMode: e.Config.DefaultDiffMode, ConfigPath: e.Path, CheckUpdates: e.Config.CheckUpdates, Runtime: runtimeOptions(e)}, e.Config)
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usage("%v", err) })
	root.SetIn(options.In)
	root.SetOut(options.Out)
	root.SetErr(options.Err)
	root.SetVersionTemplate("lazyansible {{.Version}}\n")
	pf := root.PersistentFlags()
	pf.StringVar(&f.config, "config", "", "Preferences path (or LAZYANSIBLE_CONFIG)")
	pf.StringVarP(&f.workdir, "chdir", "C", "", "Project working directory (default current directory)")
	pf.StringVar(&f.workdir, "workdir", "", "Alias for --chdir")
	pf.StringVarP(&f.inventory, "inventory", "i", "", "Inventory file or source")
	pf.StringVarP(&f.playbookDir, "playbook-dir", "d", "", "Directory to discover playbooks")
	pf.BoolVar(&f.noMouse, "no-mouse", false, "Disable dashboard mouse capture")
	pf.BoolVar(&f.notify, "notify", false, "Notify when dashboard runs finish")
	pf.BoolVar(&f.check, "check", false, "Use Ansible check mode")
	pf.BoolVar(&f.diff, "diff", false, "Show Ansible diffs")
	pf.BoolVar(&f.checkUpdates, "check-updates", true, "Check for Ansible updates in the background")
	pf.BoolVar(&f.json, "json", false, "JSON for read operations and dry-run plans; never prompt")
	root.Flags().BoolVar(&f.initConfig, "init-config", false, "Create preferences without overwriting (alias for config init)")
	root.AddCommand(configCommand(f, options), runtimeCommand(f, options), inspectCommand(f), runCommand(f, options), previewCommand(f), adhocCommand(f, options), roleCommand(f, options))
	root.AddCommand(&cobra.Command{Use: "version", Short: "Print the build version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if f.json {
			return writeJSON(cmd, map[string]string{"name": "lazyansible", "version": buildinfo.String()})
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "lazyansible", buildinfo.String())
		return err
	}})
	return root
}

// Execute emits one diagnostic and returns an appropriate process exit status.
func Execute(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	root := NewRootCommand(Options{In: in, Out: out, Err: errOut})
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	code := 1
	var exit *ExitError
	if errors.As(err, &exit) {
		code = exit.Code
	}
	if errors.Is(err, context.Canceled) {
		code = 130
	}
	// Cobra's parsing failures are usage errors, and never initialize services.
	if strings.HasPrefix(err.Error(), "unknown ") || strings.HasPrefix(err.Error(), "invalid argument") || strings.Contains(err.Error(), "accepts ") || strings.HasPrefix(err.Error(), "requires ") {
		code = 2
	}
	jsonMode, _ := root.PersistentFlags().GetBool("json")
	if jsonMode {
		_ = json.NewEncoder(root.ErrOrStderr()).Encode(map[string]any{"error": map[string]any{"code": code, "message": err.Error()}})
	} else {
		fmt.Fprintln(root.ErrOrStderr(), "lazyansible:", err)
	}
	return code
}

func loadEffective(cmd *cobra.Command, f *flags) (effective, error) {
	var e effective
	e.Path, e.Legacy = config.ResolvePath(f.config)
	if f.config != "" || os.Getenv("LAZYANSIBLE_CONFIG") != "" {
		if _, err := os.Stat(e.Path); err != nil {
			return e, usage("read config %s: %v (create it with 'config init')", e.Path, err)
		}
	}
	cfg, err := config.Load(e.Path)
	if err != nil {
		return e, usage("%v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return e, err
	}
	if f.workdir != "" {
		cwd = expandHome(f.workdir)
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return e, usage("invalid workdir: %v", err)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return e, usage("workdir: %v", err)
	}
	if !info.IsDir() {
		return e, usage("workdir is not a directory: %s", cwd)
	}
	e.WorkDir = cwd
	if cmd.Flags().Changed("inventory") {
		cfg.Inventory = f.inventory
	}
	if cmd.Flags().Changed("playbook-dir") {
		cfg.PlaybookDir = f.playbookDir
	}
	if cmd.Flags().Changed("no-mouse") {
		cfg.NoMouse = f.noMouse
	}
	if cmd.Flags().Changed("notify") {
		cfg.NotifyOnFinish = f.notify
	}
	if cmd.Flags().Changed("check") {
		cfg.DefaultCheckMode = f.check
	}
	if cmd.Flags().Changed("diff") {
		cfg.DefaultDiffMode = f.diff
	}
	if cmd.Flags().Changed("check-updates") {
		cfg.CheckUpdates = f.checkUpdates
	}
	if !strings.Contains(cfg.Inventory, ",") {
		cfg.Inventory = projectPath(cwd, cfg.Inventory)
	}
	cfg.PlaybookDir = projectPath(cwd, cfg.PlaybookDir)
	e.Config = cfg
	return e, nil
}
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return h
			}
			return filepath.Join(h, strings.TrimPrefix(p, "~/"))
		}
	}
	return p
}
func projectPath(cwd, p string) string {
	if p == "" {
		return ""
	}
	p = expandHome(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}
func runtimeOptions(e effective) ansible.RuntimeOptions {
	return ansible.RuntimeOptions{Executable: e.Config.Runtime.Executable, UVExecutable: e.Config.Runtime.UVExecutable, Package: e.Config.Runtime.Package, CacheDir: paths.CacheDir(), WorkDir: e.WorkDir}
}
func project(e effective) ansible.ProjectContext {
	return ansible.ProjectContext{WorkDir: e.WorkDir, Inventory: e.Config.Inventory, PlaybookDir: e.Config.PlaybookDir, Executable: e.Config.Runtime.Executable}
}
func writeJSON(cmd *cobra.Command, value any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
func writeYAML(cmd *cobra.Command, value any) error {
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	defer enc.Close()
	enc.SetIndent(2)
	return enc.Encode(value)
}

func dashboard(c ui.Config, cfg config.Config) error {
	app := ui.New(c)
	defer app.Close()
	app.SetNotifyOnFinish(cfg.NotifyOnFinish)
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithoutSignalHandler()}
	if !cfg.NoMouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(app, opts...)
	app.SetProgram(p)
	finished := make(chan struct{})
	defer close(finished)
	if c.Context != nil {
		go func() {
			select {
			case <-c.Context.Done():
				p.Send(ui.ShutdownMsg{})
			case <-finished:
			}
		}()
	}
	_, err := p.Run()
	if err == nil && c.Context != nil && c.Context.Err() != nil {
		return c.Context.Err()
	}
	return err
}

func approve(cmd *cobra.Command, options Options, f *flags, plan ansible.RunPlan, yes, dryRun bool) (bool, error) {
	if dryRun {
		if f.json {
			return false, writeJSON(cmd, plan)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Working directory: %s\n%s\n", plan.Command.Dir, plan.Preview)
		return false, err
	}
	if f.json {
		return false, usage("--json is read-only; add --dry-run to inspect this plan")
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Working directory: %s\n%s\n", plan.Command.Dir, plan.Preview)
	if yes {
		return true, nil
	}
	if !options.IsTerminal() {
		return false, usage("execution requires --yes without a terminal; use --dry-run to review first")
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Run this command? [y/N] ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil {
		return false, err
	}
	if strings.ToLower(strings.TrimSpace(line)) != "y" && strings.ToLower(strings.TrimSpace(line)) != "yes" {
		return false, &ExitError{130, errors.New("cancelled")}
	}
	return true, nil
}
func executePlan(cmd *cobra.Command, plan ansible.RunPlan) error {
	result, err := ansible.Execute(cmd.Context(), plan, func(event ansible.Event) {
		out := cmd.OutOrStdout()
		if event.Stream == "stderr" {
			out = cmd.ErrOrStderr()
		}
		fmt.Fprintln(out, event.Line)
	})
	if errors.Is(err, context.Canceled) {
		return err
	}
	if result.ExitCode > 0 {
		if err == nil {
			err = fmt.Errorf("Ansible command exited with status %d", result.ExitCode)
		}
		return &ExitError{result.ExitCode, err}
	}
	return err
}
