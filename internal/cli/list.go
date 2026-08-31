package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List git worktrees",
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

		// list is the one read-only command that stays useful without a
		// config at all ("bare mode", parity with M1's "works on any git
		// repo" promise): a config-less repo falls back to a bare Config
		// (no ports/urls, so those table columns are simply omitted)
		// instead of erroring or auto-scaffolding one as a side effect.
		cfg, err := config.LoadExisting(root, configPath)
		if errors.Is(err, config.ErrNoConfig) {
			cfg = &config.Config{RepoRoot: root}
		} else if err != nil {
			return err
		}

		reg, err := slots.Load(cfg.WorktreesDir)
		if err != nil {
			return err
		}

		worktrees, err := gitwt.List(root)
		if err != nil {
			return err
		}

		headers, rows := augmentListRows(filepath.Base(root), cfg, listRows(worktrees, reg.Slots()))
		renderTable(os.Stdout, stdoutIsTTY(), headers, rows)
		return nil
	},
}

// augmentListRows appends a Containers column (running container count for
// each row's compose project) to rows built by listRows, and — only when
// the config declares ports — a column for the first sorted port var
// showing its value at that row's slot. repo is the main repo's directory
// basename, used to derive each worktree's compose project name.
func augmentListRows(repo string, cfg *config.Config, rows [][]string) ([]string, [][]string) {
	headers := []string{"Slot", "Name", "Branch", "Path", "Containers"}

	vars := sortedPortVars(cfg.Ports)
	var portVar string
	if len(vars) > 0 {
		portVar = vars[0]
		headers = append(headers, portVar)
	}

	out := make([][]string, len(rows))
	for i, row := range rows {
		name := filepath.Base(row[3])
		project := repo
		if row[0] != "0" {
			project = composeProjectName(repo, name)
		}

		augmented := append([]string{}, row...)
		augmented = append(augmented, runningContainers(row[3], project))
		if portVar != "" {
			augmented = append(augmented, portColumnValue(cfg, row[0], portVar))
		}
		out[i] = augmented
	}
	return headers, out
}

// portColumnValue renders "VAR=value" for slotStr's computed value of
// portVar, or "-" when slotStr isn't a real slot (the "-" placeholder for
// a worktree missing from the registry).
func portColumnValue(cfg *config.Config, slotStr, portVar string) string {
	slot, err := strconv.Atoi(slotStr)
	if err != nil {
		return "-"
	}
	return fmt.Sprintf("%s=%d", portVar, cfg.PortValues(slot)[portVar])
}

// listRows converts worktrees into table rows: main first (slot "0"), then
// worktrees with a registered slot sorted by slot ascending, then
// worktrees present in git but missing from the registry (slot "-")
// sorted alphabetically by name.
func listRows(worktrees []gitwt.Worktree, slotByName map[string]int) [][]string {
	if len(worktrees) == 0 {
		return nil
	}

	rest := worktrees[1:]
	var withSlot, withoutSlot []gitwt.Worktree
	for _, wt := range rest {
		if _, ok := slotByName[filepath.Base(wt.Path)]; ok {
			withSlot = append(withSlot, wt)
		} else {
			withoutSlot = append(withoutSlot, wt)
		}
	}

	sort.Slice(withSlot, func(i, j int) bool {
		return slotByName[filepath.Base(withSlot[i].Path)] < slotByName[filepath.Base(withSlot[j].Path)]
	})
	sort.Slice(withoutSlot, func(i, j int) bool {
		return filepath.Base(withoutSlot[i].Path) < filepath.Base(withoutSlot[j].Path)
	})

	rows := make([][]string, 0, len(worktrees))
	rows = append(rows, worktreeRow(worktrees[0], true, "0"))
	for _, wt := range withSlot {
		rows = append(rows, worktreeRow(wt, false, strconv.Itoa(slotByName[filepath.Base(wt.Path)])))
	}
	for _, wt := range withoutSlot {
		rows = append(rows, worktreeRow(wt, false, "-"))
	}
	return rows
}

func worktreeRow(wt gitwt.Worktree, isMain bool, slot string) []string {
	name := filepath.Base(wt.Path)
	if isMain {
		name += " (main)"
	}
	branch := wt.Branch
	if branch == "" {
		branch = "(detached)"
	}
	return []string{slot, name, branch, wt.Path}
}
