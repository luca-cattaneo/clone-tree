package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

var portsCmd = &cobra.Command{
	Use:               "ports [name]",
	Short:             "Show configured ports",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(_ *cobra.Command, args []string) error {
		dir, err := cwd()
		if err != nil {
			return err
		}

		root, err := gitwt.RepoRoot(dir)
		if err != nil {
			return err
		}

		cfg, err := config.LoadExisting(root, configPath)
		if err != nil {
			return err
		}

		reg, err := slots.Load(cfg.WorktreesDir)
		if err != nil {
			return err
		}

		vars := sortedPortVars(cfg.Ports)

		if len(args) == 1 {
			name, err := resolveNameOrSlot(reg, args[0])
			if err != nil {
				return err
			}
			slot, ok := reg.Slot(name)
			if !ok {
				return fmt.Errorf("no worktree named %q", name)
			}
			renderTable(os.Stdout, stdoutIsTTY(), []string{"Var", "Value"}, portVarValueRows(cfg, slot, vars))
			return nil
		}

		headers := append([]string{"Slot", "Name"}, vars...)
		renderTable(os.Stdout, stdoutIsTTY(), headers, portsRows(reg.Slots(), cfg, vars))
		return nil
	},
}

// sortedPortVars returns the configured port var names, sorted, so table
// columns render in a stable order.
func sortedPortVars(ports map[string]config.Port) []string {
	vars := make([]string, 0, len(ports))
	for v := range ports {
		vars = append(vars, v)
	}
	sort.Strings(vars)
	return vars
}

// portVarValueRows builds the Var/Value rows for one worktree at slot, one
// row per configured port var (in vars order).
func portVarValueRows(cfg *config.Config, slot int, vars []string) [][]string {
	values := cfg.PortValues(slot)
	rows := make([][]string, len(vars))
	for i, v := range vars {
		rows[i] = []string{v, strconv.Itoa(values[v])}
	}
	return rows
}

// portsRows builds the Slot/Name/<var>... rows for every registered
// worktree plus the main repo at slot 0, sorted by slot ascending.
func portsRows(slotByName map[string]int, cfg *config.Config, vars []string) [][]string {
	type entry struct {
		name string
		slot int
	}
	entries := []entry{{name: "main", slot: 0}}
	for name, slot := range slotByName {
		entries = append(entries, entry{name: name, slot: slot})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].slot < entries[j].slot })

	rows := make([][]string, len(entries))
	for i, e := range entries {
		values := cfg.PortValues(e.slot)
		row := []string{strconv.Itoa(e.slot), e.name}
		for _, v := range vars {
			row = append(row, strconv.Itoa(values[v]))
		}
		rows[i] = row
	}
	return rows
}
