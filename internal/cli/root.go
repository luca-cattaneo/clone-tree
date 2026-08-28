// Package cli wires the ct cobra commands.
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
// /etc/hosts). A package-level var so tests can point it at a temp file
// instead of touching the real system hosts file.
var hostsPath = hosts.DefaultPath

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config.yaml (overrides auto-discovery)")
	rootCmd.AddCommand(createCmd, removeCmd, listCmd, execCmd, startCmd, stopCmd, portsCmd, hostsCmd)
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// resolveWorktree finds the worktree named name (registered under the main
// repo containing cwd) and returns it.
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

// hookAbsPath resolves a Hooks.PostCreate/PreRemove value (repo-root-
// relative, e.g. ".clone-tree/hooks/post-create.sh") to an absolute path.
// An empty rel is kept empty so hooks.Run's no-op-when-empty contract holds.
func hookAbsPath(root, rel string) string {
	if rel == "" {
		return ""
	}
	return filepath.Join(root, rel)
}
