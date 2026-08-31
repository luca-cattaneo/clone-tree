package cli

import (
	"sort"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

// completeWorktreeNames is the shared cobra.CompletionFunc for every
// command taking a single <name|slot> positional (remove, start, stop,
// exec, ports, hosts): it completes registered worktree names from the
// slots registry. Any failure along the way (not a git repo, no config yet,
// unreadable registry) yields silently no completions — shell completion
// must never surface an error or partial output, and must never scaffold a
// config as a side effect of a Tab press. Completion is only offered for
// the first positional; once one arg is already given (or, for `ct exec`,
// past its name), the shell's default completion (e.g. file names for
// exec's <cmd>) takes over instead.
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
