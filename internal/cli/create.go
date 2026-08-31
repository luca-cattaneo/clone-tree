package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/fsops"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/hooks"
	"github.com/luca-cattaneo/clone-tree/internal/hosts"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

var createBranch string

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new git worktree",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) (err error) {
		name := args[0]

		dir, err := cwd()
		if err != nil {
			return err
		}

		root, err := gitwt.RepoRoot(dir)
		if err != nil {
			return err
		}

		cfg, err := config.LoadExisting(root, configPath)
		if errors.Is(err, config.ErrNoConfig) {
			// No config yet: scaffold it via the same entry point
			// `ct create-config` uses, and stop here — a freshly
			// scaffolded config needs human review before ct acts on
			// it, so create never proceeds to provisioning in the
			// same run.
			return config.ScaffoldOrError(root)
		}
		if err != nil {
			return err
		}

		reg, err := slots.Load(cfg.WorktreesDir)
		if err != nil {
			return err
		}

		// Rollback stack: on any failure below, undo whatever already
		// succeeded, in reverse order, so a half-provisioned worktree
		// never survives a failed create.
		var rollback []func()
		defer func() {
			if err != nil {
				for i := len(rollback) - 1; i >= 0; i-- {
					rollback[i]()
				}
			}
		}()

		// Probe the candidate slot's ports before touching anything: a
		// busy port must abort cleanly, not half-provision a worktree
		// onto a port another stack already owns.
		candidateSlot, err := reg.NextFree(cfg.MaxSlots)
		if err != nil {
			return err
		}
		if busy := slots.BusyPorts(cfg.PortValues(candidateSlot)); len(busy) > 0 {
			err = fmt.Errorf("create: busy ports for slot %d: %s", candidateSlot, strings.Join(busy, ", "))
			return err
		}

		slot, err := reg.Allocate(name, cfg.MaxSlots)
		if err != nil {
			return err
		}
		rollback = append(rollback, func() { _ = reg.Free(name) })

		target := filepath.Join(cfg.WorktreesDir, name)
		if _, statErr := os.Stat(target); statErr == nil {
			err = fmt.Errorf("worktree already exists: %s", target)
			return err
		}

		if err = os.MkdirAll(cfg.WorktreesDir, 0o755); err != nil {
			return fmt.Errorf("create worktrees dir: %w", err)
		}

		branch := createBranch
		if branch == "" {
			branch = name
		}

		if err = gitwt.Create(root, target, branch); err != nil {
			return err
		}
		rollback = append(rollback, func() { _ = forceRemoveWorktree(root, target, true) })

		if err = cfg.WriteEnv(target, name, slot); err != nil {
			return err
		}

		// copy/hardlink land inside target itself, so no separate undo is
		// needed: gitwt.Remove (already on the rollback stack) deletes the
		// whole worktree, taking them with it.
		vars := cfg.InstanceVars(name, slot)
		for _, rel := range cfg.Files.Copy {
			if err = fsops.Copy(filepath.Join(root, rel), filepath.Join(target, rel)); err != nil {
				return err
			}
		}
		for _, rel := range cfg.Files.IDE {
			if err = fsops.Copy(filepath.Join(root, rel), filepath.Join(target, rel)); err != nil {
				return err
			}
		}
		for _, rel := range cfg.Files.Hardlink {
			if err = fsops.Hardlink(filepath.Join(root, rel), filepath.Join(target, rel)); err != nil {
				return err
			}
		}

		// clone_cow destinations live outside target (typically a sibling
		// datadir), so they need their own rollback entry.
		for _, cc := range cfg.Files.CloneCoW {
			dst := config.Expand(cc.Dst, vars)
			if err = fsops.CloneCoW(config.Expand(cc.Src, vars), dst); err != nil {
				return err
			}
			rollback = append(rollback, func() { _ = os.RemoveAll(dst) })
		}

		// Sibling symlinks are shared across every worktree in
		// cfg.WorktreesDir, not per-instance: idempotent to (re)create, and
		// never undone on rollback or `ct remove` — removing one would
		// break every other worktree's sibling mount.
		if err = fsops.SymlinkSiblings(cfg.Files.SymlinkSiblings, vars["projects_dir"], cfg.WorktreesDir); err != nil {
			return err
		}

		if cfg.DNSPattern != "" {
			dns := vars["dns"]
			if err = hosts.Add(hostsPath, name, dns); err != nil {
				return err
			}
			rollback = append(rollback, func() { _ = hosts.Remove(hostsPath, name) })
		}

		if err = hooks.Run(hookAbsPath(root, cfg.Hooks.PostCreate), target, hooks.Env(name, slot, vars["dns"], target, cfg.PortValues(slot))); err != nil {
			return err
		}

		fmt.Println(target)
		return nil
	},
}

func init() {
	createCmd.Flags().StringVar(&createBranch, "branch", "", "branch name (defaults to <name>)")
}
