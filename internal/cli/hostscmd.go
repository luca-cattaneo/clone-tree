package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/hosts"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

var hostsCmd = &cobra.Command{
	Use:   "hosts [name]",
	Short: "Show configured DNS entries",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
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

		headers := []string{"Slot", "Name", "DNS", "In /etc/hosts"}

		if len(args) == 1 {
			name, err := resolveNameOrSlot(reg, args[0])
			if err != nil {
				return err
			}
			slot, ok := reg.Slot(name)
			if !ok {
				return fmt.Errorf("no worktree named %q", name)
			}
			row, err := hostsRow(cfg, name, slot)
			if err != nil {
				return err
			}
			renderTable(os.Stdout, stdoutIsTTY(), headers, [][]string{row})
			return nil
		}

		rows, err := hostsRows(reg.Slots(), cfg)
		if err != nil {
			return err
		}
		renderTable(os.Stdout, stdoutIsTTY(), headers, rows)
		return nil
	},
}

// hostsRows builds the Slot/Name/DNS/In-/etc/hosts rows for every
// registered worktree, sorted by slot ascending.
func hostsRows(slotByName map[string]int, cfg *config.Config) ([][]string, error) {
	type entry struct {
		name string
		slot int
	}
	entries := make([]entry, 0, len(slotByName))
	for name, slot := range slotByName {
		entries = append(entries, entry{name: name, slot: slot})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].slot < entries[j].slot })

	rows := make([][]string, len(entries))
	for i, e := range entries {
		row, err := hostsRow(cfg, e.name, e.slot)
		if err != nil {
			return nil, err
		}
		rows[i] = row
	}
	return rows, nil
}

// hostsRow builds one Slot/Name/DNS/In-/etc/hosts row: DNS is cfg.DNSPattern
// expanded for name/slot, and the last column is "✓"/"-" from hosts.Has
// against the package-level hostsPath.
func hostsRow(cfg *config.Config, name string, slot int) ([]string, error) {
	dns := cfg.InstanceVars(name, slot)["dns"]
	present, err := hosts.Has(hostsPath, name)
	if err != nil {
		return nil, err
	}
	mark := "-"
	if present {
		mark = "✓"
	}
	return []string{strconv.Itoa(slot), name, dns, mark}, nil
}
