package cli

import (
	"errors"
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

		headers, rows := augmentListRows(filepath.Base(root), listRows(worktrees, reg.Slots()))
		renderTable(os.Stdout, stdoutIsTTY(), headers, rows)
		return nil
	},
}

func augmentListRows(repo string, rows [][]string) ([]string, [][]string) {
	headers := []string{"Slot", "Name", "Branch", "Path", "Containers"}

	out := make([][]string, len(rows))
	for i, row := range rows {
		name := filepath.Base(row[3])
		project := repo
		if row[0] != "0" {
			project = composeProjectName(repo, name)
		}

		augmented := append([]string{}, row...)
		augmented = append(augmented, runningContainers(row[3], project))
		out[i] = augmented
	}
	return headers, out
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
