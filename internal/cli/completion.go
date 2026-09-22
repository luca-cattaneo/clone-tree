package cli

import (
	"sort"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

func completeWorktreeNames(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}

	dir, err := cwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	root, err := gitwt.RepoRoot(dir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := config.LoadExisting(root, configPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	reg, err := slots.Load(cfg.WorktreesDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names := make([]string, 0, len(reg.Slots()))
	for name := range reg.Slots() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}
