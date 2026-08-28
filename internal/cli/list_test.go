package cli

import (
	"reflect"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
)

func TestListRows_GIVEN_mainAndRegisteredWorktrees_WHEN_converted_THEN_mainIsSlotZeroThenAscendingBySlot(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/clone-tree", Branch: "main"},
		{Path: "/repo/clone-tree-worktrees/zeta", Branch: "zeta-branch"},
		{Path: "/repo/clone-tree-worktrees/alpha", Branch: "alpha-branch"},
	}
	slotByName := map[string]int{"zeta": 1, "alpha": 2}

	got := listRows(worktrees, slotByName)

	want := [][]string{
		{"0", "clone-tree (main)", "main", "/repo/clone-tree"},
		{"1", "zeta", "zeta-branch", "/repo/clone-tree-worktrees/zeta"},
		{"2", "alpha", "alpha-branch", "/repo/clone-tree-worktrees/alpha"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestListRows_GIVEN_worktreeMissingFromRegistry_WHEN_converted_THEN_placeholderSlotSortedAfterRegisteredOnes(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/clone-tree", Branch: "main"},
		{Path: "/repo/clone-tree-worktrees/orphan", Branch: "orphan-branch"},
		{Path: "/repo/clone-tree-worktrees/registered", Branch: "registered-branch"},
	}
	slotByName := map[string]int{"registered": 5}

	got := listRows(worktrees, slotByName)

	want := [][]string{
		{"0", "clone-tree (main)", "main", "/repo/clone-tree"},
		{"5", "registered", "registered-branch", "/repo/clone-tree-worktrees/registered"},
		{"-", "orphan", "orphan-branch", "/repo/clone-tree-worktrees/orphan"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestListRows_GIVEN_multipleWorktreesMissingFromRegistry_WHEN_converted_THEN_sortedAlphabeticallyAmongThemselves(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/clone-tree", Branch: "main"},
		{Path: "/repo/clone-tree-worktrees/zeta", Branch: "zeta-branch"},
		{Path: "/repo/clone-tree-worktrees/alpha", Branch: "alpha-branch"},
	}

	got := listRows(worktrees, map[string]int{})

	want := [][]string{
		{"0", "clone-tree (main)", "main", "/repo/clone-tree"},
		{"-", "alpha", "alpha-branch", "/repo/clone-tree-worktrees/alpha"},
		{"-", "zeta", "zeta-branch", "/repo/clone-tree-worktrees/zeta"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestListRows_GIVEN_detachedWorktree_WHEN_converted_THEN_branchColumnShowsDetachedMarker(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/clone-tree", Branch: "main"},
		{Path: "/repo/clone-tree-worktrees/detached", Branch: ""},
	}

	got := listRows(worktrees, map[string]int{})

	if got[1][2] != "(detached)" {
		t.Fatalf("got branch column %q, want (detached)", got[1][2])
	}
}

func TestListRows_GIVEN_noWorktrees_WHEN_converted_THEN_noRows(t *testing.T) {
	got := listRows(nil, map[string]int{})
	if got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}
