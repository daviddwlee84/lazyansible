package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/daviddwlee84/lazyansible/internal/config"
	"github.com/daviddwlee84/lazyansible/internal/editor"
)

func initConfig(cmd *cobra.Command, f *flags) error {
	path := f.config
	if path == "" {
		path = config.DefaultPath()
	}
	if err := config.WriteExample(path); err != nil {
		return fmt.Errorf("create config %s: %w", path, err)
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Config created:", path)
	return err
}
func configCommand(f *flags, options Options) *cobra.Command {
	group := commandGroup(f, "config", "Inspect and edit lazyansible preferences")
	group.AddCommand(&cobra.Command{Use: "init", Short: "Create an annotated config without overwriting", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if f.json {
			return usage("config init does not support --json")
		}
		return initConfig(cmd, f)
	}})
	group.AddCommand(&cobra.Command{Use: "show", Short: "Show effective preferences and their selected path", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		if f.json {
			return writeJSON(cmd, e)
		}
		return writeYAML(cmd, e)
	}})
	group.AddCommand(&cobra.Command{Use: "edit", Short: "Open preferences in VISUAL or EDITOR and validate afterward", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if f.json || !options.IsTerminal() {
			return usage("config edit requires a terminal and does not support --json")
		}
		path, _ := config.ResolvePath(f.config)
		value := os.Getenv("VISUAL")
		if value == "" {
			value = os.Getenv("EDITOR")
		}
		if value == "" {
			for _, candidate := range []string{"nano", "vim", "vi"} {
				if _, err := exec.LookPath(candidate); err == nil {
					value = candidate
					break
				}
			}
		}
		editor, err := editor.Command(value, path)
		if err != nil {
			return err
		}
		if editor.Err != nil {
			return editor.Err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := config.WriteExample(path); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		editor.Stdin = cmd.InOrStdin()
		editor.Stdout = cmd.OutOrStdout()
		editor.Stderr = cmd.ErrOrStderr()
		if err := editor.Run(); err != nil {
			return err
		}
		if _, err := config.Load(path); err != nil {
			return fmt.Errorf("saved edits retained; %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Config valid:", path)
		return err
	}})
	return group
}

func runtimeCommand(f *flags, options Options) *cobra.Command {
	group := commandGroup(f, "runtime", "Inspect or explicitly manage the shared Ansible uv tool")
	group.AddCommand(&cobra.Command{Use: "status", Short: "Show the selected Ansible executable, version, Python, and owner", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		status, err := ansible.Status(cmd.Context(), runtimeOptions(e))
		if err != nil {
			return err
		}
		if f.json {
			return writeJSON(cmd, status)
		}
		return writeYAML(cmd, status)
	}})
	group.AddCommand(&cobra.Command{Use: "check", Short: "Check available Ansible versions without upgrading", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		status, err := ansible.Check(cmd.Context(), runtimeOptions(e), true)
		if err != nil {
			return err
		}
		if f.json {
			return writeJSON(cmd, status)
		}
		return writeYAML(cmd, status)
	}})
	var installYes, installDry bool
	var python string
	install := &cobra.Command{Use: "install [PACKAGE_SPEC]", Short: "Install Ansible as a shared uv tool after reviewing the command", Example: "  lazyansible runtime install 'ansible-core==2.20.5' --python 3.13 --dry-run\n  lazyansible runtime install ansible-core --yes", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		opts := runtimeOptions(e)
		opts.Python = python
		spec := opts.Package
		if spec == "" {
			spec = "ansible-core"
		}
		if len(args) > 0 {
			spec = args[0]
		}
		plan, err := ansible.PrepareInstall(cmd.Context(), opts, spec)
		if err != nil {
			return err
		}
		run, err := approve(cmd, options, f, plan, installYes, installDry)
		if err != nil || !run {
			return err
		}
		return executePlan(cmd, plan)
	}}
	install.Flags().BoolVarP(&installYes, "yes", "y", false, "Execute the reviewed installation without prompting")
	install.Flags().BoolVar(&installDry, "dry-run", false, "Print the installation plan without executing")
	install.Flags().StringVar(&python, "python", "", "Python request for a new tool environment (for example 3.13)")
	group.AddCommand(install)
	var upgradeYes, upgradeDry bool
	upgrade := &cobra.Command{Use: "upgrade", Short: "Upgrade only the selected uv-owned Ansible tool", Long: "Upgrade only the selected uv-owned Ansible tool, preserving its existing constraints.\nThis does not upgrade lazyansible, uv itself, other tools, or collections.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		plan, err := ansible.PrepareUpgrade(cmd.Context(), runtimeOptions(e))
		if err != nil {
			return err
		}
		run, err := approve(cmd, options, f, plan, upgradeYes, upgradeDry)
		if err != nil || !run {
			return err
		}
		return executePlan(cmd, plan)
	}}
	upgrade.Flags().BoolVarP(&upgradeYes, "yes", "y", false, "Execute the reviewed upgrade without prompting")
	upgrade.Flags().BoolVar(&upgradeDry, "dry-run", false, "Print the upgrade plan without executing")
	group.AddCommand(upgrade)
	return group
}

func inspectCommand(f *flags) *cobra.Command {
	group := commandGroup(f, "inspect", "Read Ansible inventory and configuration in project context")
	group.AddCommand(inspectTagsCommand(f))
	group.AddCommand(&cobra.Command{Use: "inventory", Short: "Read resolved hosts, groups, and redacted inventory variables", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		result, err := ansible.InspectInventory(cmd.Context(), project(e))
		if err != nil {
			return err
		}
		if f.json {
			return writeJSON(cmd, result)
		}
		return writeYAML(cmd, result)
	}})
	group.AddCommand(&cobra.Command{Use: "config", Short: "Read changed Ansible settings and their reported origins", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		result, err := ansible.InspectConfig(cmd.Context(), project(e))
		if err != nil {
			return err
		}
		if f.json {
			return writeJSON(cmd, result)
		}
		return writeYAML(cmd, result)
	}})
	return group
}

type runFlags struct {
	yes, dryRun, become                     bool
	limit, tags, vault, hosts, module, args string
	extra                                   []string
}

func addRunFlags(cmd *cobra.Command, r *runFlags) {
	cmd.Flags().BoolVarP(&r.yes, "yes", "y", false, "Execute after printing the command without prompting")
	cmd.Flags().BoolVar(&r.dryRun, "dry-run", false, "Print the validated, redacted plan without executing")
	addScopeFlags(cmd, r, true)
}

func addScopeFlags(cmd *cobra.Command, r *runFlags, includeTags bool) {
	cmd.Flags().BoolVarP(&r.become, "become", "b", false, "Request Ansible privilege escalation")
	cmd.Flags().StringVarP(&r.limit, "limit", "l", "", "Limit execution to matching hosts")
	if includeTags {
		cmd.Flags().StringVarP(&r.tags, "tags", "t", "", "Comma-separated Ansible tags")
	}
	cmd.Flags().StringArrayVarP(&r.extra, "extra-vars", "e", nil, "Extra variables or @file (repeatable; hidden from previews)")
	cmd.Flags().StringVar(&r.vault, "vault-password-file", "", "Existing vault password file (not copied into history)")
}
func runCommand(f *flags, options Options) *cobra.Command {
	r := &runFlags{}
	cmd := &cobra.Command{Use: "run PLAYBOOK", Short: "Review and run a playbook", Example: "  lazyansible -C ./ansible run playbooks/site.yml --check --diff --dry-run\n  lazyansible -C ./ansible run playbooks/site.yml --limit staging --yes", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runRequest(cmd, options, f, r, "playbook", args[0])
	}}
	addRunFlags(cmd, r)
	return cmd
}
func adhocCommand(f *flags, options Options) *cobra.Command {
	r := &runFlags{}
	cmd := &cobra.Command{Use: "adhoc HOST_PATTERN", Short: "Review and run an ad-hoc module", Example: "  lazyansible -C ./ansible adhoc all -m ansible.builtin.ping --dry-run", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(r.module) == "" {
			return usage("adhoc requires --module; example: lazyansible adhoc all -m ansible.builtin.ping --dry-run")
		}
		r.hosts = args[0]
		return runRequest(cmd, options, f, r, "adhoc", "")
	}}
	addRunFlags(cmd, r)
	cmd.Flags().StringVarP(&r.module, "module", "m", "", "Ansible module name")
	cmd.Flags().StringVarP(&r.args, "args", "a", "", "Module arguments (hidden from previews)")
	return cmd
}
func roleCommand(f *flags, options Options) *cobra.Command {
	r := &runFlags{}
	group := commandGroup(f, "role", "Run a local role through the shared execution path")
	cmd := &cobra.Command{Use: "run ROLE_PATH", Short: "Review and run a local role against selected hosts", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error { return runRequest(cmd, options, f, r, "role", args[0]) }}
	addRunFlags(cmd, r)
	cmd.Flags().StringVar(&r.hosts, "hosts", "all", "Host pattern for the generated role play")
	group.AddCommand(cmd)
	return group
}
func runRequest(cmd *cobra.Command, options Options, f *flags, r *runFlags, kind, target string) error {
	e, err := loadEffective(cmd, f)
	if err != nil {
		return err
	}
	req := requestFromFlags(e, r, kind, target)
	plan, err := ansible.Prepare(cmd.Context(), req)
	if err != nil {
		return err
	}
	run, err := approve(cmd, options, f, plan, r.yes, r.dryRun)
	if err != nil || !run {
		return err
	}
	return executePlan(cmd, plan)
}

func requestFromFlags(e effective, r *runFlags, kind, target string) ansible.RunRequest {
	req := ansible.RunRequest{Kind: kind, Project: project(e), Hosts: r.hosts, Module: r.module, Args: r.args, Limit: r.limit, Tags: r.tags, Check: e.Config.DefaultCheckMode, Diff: e.Config.DefaultDiffMode, Become: r.become, ExtraVars: r.extra, VaultPasswordFile: r.vault, Executable: e.Config.Runtime.Executable}
	switch kind {
	case "role":
		req.RolePath = target
	case "playbook":
		req.Playbook = target
	}
	return req
}

func commandGroup(f *flags, use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if f.json {
			return usage("select a read subcommand before using --json")
		}
		return cmd.Help()
	}}
}
