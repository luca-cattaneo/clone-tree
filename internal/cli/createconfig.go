package cli

import (
	"errors"
	"fmt"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/spf13/cobra"
)

var createConfigCmd = &cobra.Command{
	Use:   "create-config",
	Short: "Scaffold .clone-tree/config.yaml without creating a worktree",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		dir, err := cwd()
		if err != nil {
			return err
		}

		root, err := gitwt.RepoRoot(dir)
		if err != nil {
			return err
		}

		_, err = config.LoadExisting(root, configPath)
		switch {
		case err == nil:
			return fmt.Errorf("config already exists: %s", config.ResolvePath(root, configPath))
		case errors.Is(err, config.ErrNoConfig):
			return printScaffoldNotice(config.ScaffoldOrError(root))
		default:
			return err
		}
	},
}

func printScaffoldNotice(err error) error {
	var scaffolded *config.ScaffoldedError
	if errors.As(err, &scaffolded) {
		fmt.Println(scaffolded.Error())
		return nil
	}
	return err
}
