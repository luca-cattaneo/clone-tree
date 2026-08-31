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
	Use:               "hosts [name]",
	Short:             "Show configured DNS entries",
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
			if len(cfg.Urls) > 0 {
				renderTable(os.Stdout, stdoutIsTTY(), []string{"Service", "URL"}, urlRows(cfg, name, slot))
			}
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
// expanded for name/slot, and the last column is "✓" when hosts.Has reports
// a clone-tree-owned entry, "✓ (unmanaged)" when no owned entry exists but
// hosts.HasDNS still finds dns bound by some other (non-ct) line, or "-"
// when dns isn't present in the hosts file at all — all checked against the
// package-level hostsPath.
func hostsRow(cfg *config.Config, name string, slot int) ([]string, error) {
	dns := cfg.InstanceVars(name, slot)["dns"]
	owned, err := hosts.Has(hostsPath, name)
	if err != nil {
		return nil, err
	}

	mark := "-"
	switch {
	case owned:
		mark = "✓"
	default:
		unmanaged, err := hosts.HasDNS(hostsPath, dns)
		if err != nil {
			return nil, err
		}
		if unmanaged {
			mark = "✓ (unmanaged)"
		}
	}

	return []string{strconv.Itoa(slot), name, dns, mark}, nil
}

// urlRows builds the Service/URL rows for cfg.Urls, sorted by label, with
// every {var} in each URL template expanded via cfg.InstanceVars(name,
// slot) (port vars + {dns} + the other instance vars) exactly like an env:
// value.
func urlRows(cfg *config.Config, name string, slot int) [][]string {
	vars := cfg.InstanceVars(name, slot)

	labels := make([]string, 0, len(cfg.Urls))
	for label := range cfg.Urls {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	rows := make([][]string, len(labels))
	for i, label := range labels {
		rows[i] = []string{label, config.Expand(cfg.Urls[label], vars)}
	}
	return rows
}
