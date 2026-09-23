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

		if _, err := config.LoadExisting(root, configPath); err != nil && !errors.Is(err, config.ErrNoConfig) {
			return err
		}

		reg, err := slots.Load(root)
		if err != nil {
			return err
		}

		worktrees, err := gitwt.List(root)
		if err != nil {
			return err
		}

		headers, rows := augmentListRows(root, listRows(root, worktrees, reg.Slots()))
		renderTable(os.Stdout, stdoutIsTTY(), headers, rows)
		return nil
	},
}

func augmentListRows(root string, rows [][]string) ([]string, [][]string) {
	headers := []string{"Slot", "Name", "Branch", "Path", "Containers"}
	repo := filepath.Base(root)

	out := make([][]string, len(rows))
	for i, row := range rows {
		project := repo
		if row[0] != "0" {
			project = composeProjectName(repo, gitwt.WorktreeName(root, row[3]))
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
func listRows(root string, worktrees []gitwt.Worktree, slotByName map[string]int) [][]string {
	if len(worktrees) == 0 {
		return nil
	}

	rest := worktrees[1:]
	var withSlot, withoutSlot []gitwt.Worktree
	for _, wt := range rest {
		if _, ok := slotByName[gitwt.WorktreeName(root, wt.Path)]; ok {
			withSlot = append(withSlot, wt)
		} else {
			withoutSlot = append(withoutSlot, wt)
		}
	}

	sort.Slice(withSlot, func(i, j int) bool {
		return slotByName[gitwt.WorktreeName(root, withSlot[i].Path)] < slotByName[gitwt.WorktreeName(root, withSlot[j].Path)]
	})
	sort.Slice(withoutSlot, func(i, j int) bool {
		return gitwt.WorktreeName(root, withoutSlot[i].Path) < gitwt.WorktreeName(root, withoutSlot[j].Path)
	})

	rows := make([][]string, 0, len(worktrees))
	rows = append(rows, worktreeRow(root, worktrees[0], true, "0"))
	for _, wt := range withSlot {
		rows = append(rows, worktreeRow(root, wt, false, strconv.Itoa(slotByName[gitwt.WorktreeName(root, wt.Path)])))
	}
	for _, wt := range withoutSlot {
		rows = append(rows, worktreeRow(root, wt, false, "-"))
	}
	return rows
}

func worktreeRow(root string, wt gitwt.Worktree, isMain bool, slot string) []string {
	name := gitwt.WorktreeName(root, wt.Path)
	if isMain {
		name += " (main)"
	}
	branch := wt.Branch
	if branch == "" {
		branch = "(detached)"
	}
	return []string{slot, name, branch, wt.Path}
}
