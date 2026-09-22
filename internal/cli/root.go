package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/hosts"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "ct",
	Short:         "clone-tree: zero-config git worktree manager",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// configPath is the --config escape hatch: an explicit path to
// config.yaml, bypassing auto-discovery (<git-toplevel>/.clone-tree/config.yaml)
// and auto-scaffold.
var configPath string

// hostsPath is the hosts file ct manages (normally hosts.DefaultPath, i.e.
// /etc/hosts).
var hostsPath = hosts.DefaultPath

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "path to config.yaml (overrides auto-discovery)")
	rootCmd.AddCommand(createCmd, createConfigCmd, removeCmd, listCmd, startCmd, stopCmd, portsCmd, hostsCmd, doctorCmd)
}

func Execute() error {
	return rootCmd.Execute()
}

func resolveWorktree(cwd, name string) (gitwt.Worktree, error) {
	worktrees, err := gitwt.List(cwd)
	if err != nil {
		return gitwt.Worktree{}, err
	}
	wt, ok := gitwt.FindByName(worktrees, name)
	if !ok {
		return gitwt.Worktree{}, fmt.Errorf("no worktree named %q", name)
	}
	return wt, nil
}

func cwd() (string, error) {
	return os.Getwd()
}

// hookAbsPath resolves a repo-root-relative hook path (e.g.
// ".clone-tree/hooks/post-create.sh") to an absolute path. An empty rel is
// kept empty, since hooks.Run treats "" as no-op.
func hookAbsPath(root, rel string) string {
	if rel == "" {
		return ""
	}
	return filepath.Join(root, rel)
}
