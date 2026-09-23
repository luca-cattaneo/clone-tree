package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
)

func TestListCmd_GIVEN_noConfigInRepo_WHEN_run_THEN_bareModeNoErrorNoScaffold(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	chdir(t, repoDir)
	configPath = ""

	if err := listCmd.RunE(listCmd, nil); err != nil {
		t.Fatalf("list in bare mode: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(repoDir, ".clone-tree")); !os.IsNotExist(statErr) {
		t.Fatalf("expected list to never scaffold .clone-tree/, stat err: %v", statErr)
	}
}

func TestListRows_GIVEN_mainAndRegisteredWorktrees_WHEN_converted_THEN_mainIsSlotZeroThenAscendingBySlot(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/clone-tree", Branch: "main"},
		{Path: "/repo/clone-tree-zeta", Branch: "zeta-branch"},
		{Path: "/repo/clone-tree-alpha", Branch: "alpha-branch"},
	}
	slotByName := map[string]int{"zeta": 1, "alpha": 2}

	got := listRows("/repo/clone-tree", worktrees, slotByName)

	want := [][]string{
		{"0", "clone-tree (main)", "main", "/repo/clone-tree"},
		{"1", "zeta", "zeta-branch", "/repo/clone-tree-zeta"},
		{"2", "alpha", "alpha-branch", "/repo/clone-tree-alpha"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestListRows_GIVEN_worktreeMissingFromRegistry_WHEN_converted_THEN_placeholderSlotSortedAfterRegisteredOnes(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/clone-tree", Branch: "main"},
		{Path: "/repo/clone-tree-orphan", Branch: "orphan-branch"},
		{Path: "/repo/clone-tree-registered", Branch: "registered-branch"},
	}
	slotByName := map[string]int{"registered": 5}

	got := listRows("/repo/clone-tree", worktrees, slotByName)

	want := [][]string{
		{"0", "clone-tree (main)", "main", "/repo/clone-tree"},
		{"5", "registered", "registered-branch", "/repo/clone-tree-registered"},
		{"-", "orphan", "orphan-branch", "/repo/clone-tree-orphan"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestListRows_GIVEN_multipleWorktreesMissingFromRegistry_WHEN_converted_THEN_sortedAlphabeticallyAmongThemselves(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/clone-tree", Branch: "main"},
		{Path: "/repo/clone-tree-zeta", Branch: "zeta-branch"},
		{Path: "/repo/clone-tree-alpha", Branch: "alpha-branch"},
	}

	got := listRows("/repo/clone-tree", worktrees, map[string]int{})

	want := [][]string{
		{"0", "clone-tree (main)", "main", "/repo/clone-tree"},
		{"-", "alpha", "alpha-branch", "/repo/clone-tree-alpha"},
		{"-", "zeta", "zeta-branch", "/repo/clone-tree-zeta"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestListRows_GIVEN_detachedWorktree_WHEN_converted_THEN_branchColumnShowsDetachedMarker(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/clone-tree", Branch: "main"},
		{Path: "/repo/clone-tree-detached", Branch: ""},
	}

	got := listRows("/repo/clone-tree", worktrees, map[string]int{})

	if got[1][2] != "(detached)" {
		t.Fatalf("got branch column %q, want (detached)", got[1][2])
	}
}

func TestListRows_GIVEN_noWorktrees_WHEN_converted_THEN_noRows(t *testing.T) {
	got := listRows("/repo/clone-tree", nil, map[string]int{})
	if got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}
