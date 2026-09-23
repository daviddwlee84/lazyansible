package cli

import (
	"fmt"

	"github.com/daviddwlee84/lazyansible/internal/ansible"
	"github.com/spf13/cobra"
)

func previewCommand(f *flags) *cobra.Command {
	r := &runFlags{}
	cmd := &cobra.Command{Use: "preview PLAYBOOK", Short: "List selected hosts, tasks and tags without executing playbook tasks", Long: "Observe a playbook using Ansible's native list options. Dynamic includes and runtime conditions remain unknown.\nThis is distinct from run --dry-run (command plan only) and --check (execution in check mode).", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		plan, err := ansible.Prepare(cmd.Context(), requestFromFlags(e, r, "playbook", args[0]))
		if err != nil {
			return err
		}
		result, observeErr := ansible.Preview(cmd.Context(), plan)
		if f.json {
			err = writeJSON(cmd, result)
		} else {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n%s", result.Notice, result.Command, result.Output)
			if result.Diagnostics != "" {
				fmt.Fprint(cmd.ErrOrStderr(), result.Diagnostics)
			}
		}
		if err != nil {
			return err
		}
		return listingError(result.ExitCode, observeErr)
	}}
	addScopeFlags(cmd, r, true)
	return cmd
}

func inspectTagsCommand(f *flags) *cobra.Command {
	r := &runFlags{}
	cmd := &cobra.Command{Use: "tags PLAYBOOK", Short: "Discover playbook tags under the current Ansible configuration", Long: "List Ansible-discovered tags without a UI/CLI tag selection. Inherited Ansible filters still apply, and dynamic includes may add tags at runtime.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := loadEffective(cmd, f)
		if err != nil {
			return err
		}
		plan, err := ansible.Prepare(cmd.Context(), requestFromFlags(e, r, "playbook", args[0]))
		if err != nil {
			return err
		}
		result, observeErr := ansible.DiscoverTags(cmd.Context(), plan)
		if f.json {
			err = writeJSON(cmd, result)
		} else {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n%s", result.Notice, result.Command, result.Output)
			if result.Diagnostics != "" {
				fmt.Fprint(cmd.ErrOrStderr(), result.Diagnostics)
			}
		}
		if err != nil {
			return err
		}
		return listingError(result.ExitCode, observeErr)
	}}
	addScopeFlags(cmd, r, false)
	return cmd
}

func listingError(exitCode int, err error) error {
	if err == nil {
		return nil
	}
	if exitCode > 0 {
		return &ExitError{Code: exitCode, Err: err}
	}
	return err
}
