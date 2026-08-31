package cli

import (
	"fmt"
	"os"

	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:               "exec <name> -- <cmd...>",
	Short:             "Run a command in a worktree directory",
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("usage: ct exec <name> -- <cmd...>")
		}

		name := args[0]
		command := args[1:]

		dir, err := cwd()
		if err != nil {
			return err
		}

		wt, err := resolveWorktree(dir, name)
		if err != nil {
			return err
		}

		code, err := gitwt.Exec(wt.Path, command)
		if err != nil {
			return err
		}
		if code != 0 {
			os.Exit(code)
		}
		return nil
	},
}
