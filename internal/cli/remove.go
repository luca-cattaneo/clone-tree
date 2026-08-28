package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/hooks"
	"github.com/luca-cattaneo/clone-tree/internal/hosts"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

var removeForce bool

var removeCmd = &cobra.Command{
	Use:   "remove <name|slot>",
	Short: "Remove a git worktree",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		arg := args[0]

		dir, err := cwd()
		if err != nil {
			return err
		}

		root, err := gitwt.RepoRoot(dir)
		if err != nil {
			return err
		}

		cfg, err := config.Load(root, configPath)
		if err != nil {
			return err
		}

		reg, err := slots.Load(cfg.WorktreesDir)
		if err != nil {
			return err
		}

		name, err := resolveNameOrSlot(reg, arg)
		if err != nil {
			return err
		}

		wt, err := resolveWorktree(root, name)
		if err != nil {
			return err
		}

		// pre_remove runs first, before anything is torn down — the repo's
		// escape hatch for cleanup that must see the instance still intact
		// (e.g. reading its DB before the compose stack goes away).
		if slot, ok := reg.Slot(name); ok {
			vars := cfg.InstanceVars(name, slot)
			if err := hooks.Run(hookAbsPath(root, cfg.Hooks.PreRemove), wt.Path, hooks.Env(name, slot, vars["dns"], wt.Path, cfg.PortValues(slot))); err != nil {
				return err
			}
		}

		project := composeProjectName(filepath.Base(root), name)
		if err := composeDown(wt.Path, project); err != nil {
			return err
		}

		if err := gitwt.Remove(root, wt.Path, removeForce); err != nil {
			return err
		}

		if slot, ok := reg.Slot(name); ok {
			removeCloneCoWDsts(cfg, name, slot)
			if cfg.DNSPattern != "" {
				if err := hosts.Remove(hostsPath, name); err != nil {
					return err
				}
			}
		}

		return reg.Free(name)
	},
}

// removeCloneCoWDsts deletes every files.clone_cow destination templated
// for name/slot, printing what it removed. clone_cow destinations
// typically live outside the worktree itself (a sibling datadir), so
// gitwt.Remove doesn't reach them. A missing destination is not an
// error — it may never have been created, or already removed by a prior
// failed create's rollback.
func removeCloneCoWDsts(cfg *config.Config, name string, slot int) {
	vars := cfg.InstanceVars(name, slot)
	for _, cc := range cfg.Files.CloneCoW {
		dst := config.Expand(cc.Dst, vars)
		if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.RemoveAll(dst); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "ct: warning: failed to remove %s: %v\n", dst, err)
			continue
		}
		fmt.Println("removed", dst)
	}
}

// resolveNameOrSlot accepts either a worktree name or a slot number
// (parity with tagpay-worktree.sh). Slot 0 is the main repo and can never
// be removed this way.
func resolveNameOrSlot(reg *slots.Registry, arg string) (string, error) {
	slot, err := strconv.Atoi(arg)
	if err != nil {
		return arg, nil
	}
	if slot == 0 {
		return "", fmt.Errorf("slot 0 is the main repository")
	}
	name, ok := reg.Name(slot)
	if !ok {
		return "", fmt.Errorf("no worktree registered at slot %d", slot)
	}
	return name, nil
}

func init() {
	// Defaults to true: every worktree created by `ct create` carries a
	// generated .env, which git worktree remove otherwise refuses to
	// remove as "untracked content" — clone-tree worktrees are disposable
	// by design. Pass --force=false to fall back to git's safe default.
	removeCmd.Flags().BoolVar(&removeForce, "force", true, "force removal of the worktree")
}
