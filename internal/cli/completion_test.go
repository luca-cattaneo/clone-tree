package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompleteWorktreeNames_GIVEN_registeredWorktrees_WHEN_completedForFirstArg_THEN_sortedNamesReturned(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	configYAML := "version: 1\n" +
		"worktrees_dir: ../repo-worktrees\n" +
		"base_branch: main\n" +
		"dns_pattern: \"\"\n" +
		"max_slots: 9\n" +
		"ports: {}\n" +
		"env: {}\n" +
		"files: {}\n" +
		"hooks: {}\n"
	if err := os.MkdirAll(filepath.Join(repoDir, ".clone-tree"), 0o755); err != nil {
		t.Fatalf("MkdirAll .clone-tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".clone-tree", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	chdir(t, repoDir)
	configPath = ""
	createBranch = ""

	if err := createCmd.RunE(createCmd, []string{"zeta"}); err != nil {
		t.Fatalf("create zeta: %v", err)
	}
	if err := createCmd.RunE(createCmd, []string{"alpha"}); err != nil {
		t.Fatalf("create alpha: %v", err)
	}

	got, directive := completeWorktreeNames(removeCmd, nil, "")
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("got directive %v, want ShellCompDirectiveNoFileComp", directive)
	}
}

func TestCompleteWorktreeNames_GIVEN_noConfigInRepo_WHEN_completed_THEN_silentlyNoCompletions(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	chdir(t, repoDir)
	configPath = ""

	got, directive := completeWorktreeNames(removeCmd, nil, "")
	if got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("got directive %v, want ShellCompDirectiveNoFileComp", directive)
	}
}

func TestCompleteWorktreeNames_GIVEN_argAlreadyGiven_WHEN_completed_THEN_fallsBackToDefaultCompletion(t *testing.T) {
	got, directive := completeWorktreeNames(removeCmd, []string{"feature"}, "")
	if got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
	if directive != cobra.ShellCompDirectiveDefault {
		t.Fatalf("got directive %v, want ShellCompDirectiveDefault", directive)
	}
}
