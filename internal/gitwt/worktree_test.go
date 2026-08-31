package gitwt_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
)

func TestParsePorcelain_GIVEN_mainAndLinkedWorktree_WHEN_parsed_THEN_bothEntriesExtracted(t *testing.T) {
	input := "worktree /repo/main\n" +
		"HEAD abcd1234abcd1234abcd1234abcd1234abcd1234\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /repo/main-worktrees/feature\n" +
		"HEAD 1111111111111111111111111111111111111111\n" +
		"branch refs/heads/feature\n" +
		"\n"

	got := gitwt.ParsePorcelain(input)

	want := []gitwt.Worktree{
		{Path: "/repo/main", Branch: "main"},
		{Path: "/repo/main-worktrees/feature", Branch: "feature"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParsePorcelain_GIVEN_detachedWorktree_WHEN_parsed_THEN_detachedFlagSetAndBranchEmpty(t *testing.T) {
	input := "worktree /repo/main-worktrees/detached\n" +
		"HEAD 2222222222222222222222222222222222222222\n" +
		"detached\n" +
		"\n"

	got := gitwt.ParsePorcelain(input)

	want := []gitwt.Worktree{
		{Path: "/repo/main-worktrees/detached", Detached: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParsePorcelain_GIVEN_noTrailingBlankLine_WHEN_parsed_THEN_lastEntryStillFlushed(t *testing.T) {
	input := "worktree /repo/main\n" +
		"HEAD abcd1234abcd1234abcd1234abcd1234abcd1234\n" +
		"branch refs/heads/main"

	got := gitwt.ParsePorcelain(input)

	if len(got) != 1 || got[0].Path != "/repo/main" || got[0].Branch != "main" {
		t.Fatalf("got %#v", got)
	}
}

func TestParsePorcelain_GIVEN_emptyOutput_WHEN_parsed_THEN_emptySlice(t *testing.T) {
	got := gitwt.ParsePorcelain("")
	if len(got) != 0 {
		t.Fatalf("got %#v, want empty", got)
	}
}

func TestFindByName_GIVEN_matchingBasename_WHEN_searched_THEN_found(t *testing.T) {
	worktrees := []gitwt.Worktree{
		{Path: "/repo/main", Branch: "main"},
		{Path: "/repo/main-worktrees/feature", Branch: "feature"},
	}

	got, ok := gitwt.FindByName(worktrees, "feature")
	if !ok {
		t.Fatalf("expected to find feature")
	}
	if got.Path != "/repo/main-worktrees/feature" {
		t.Fatalf("got %#v", got)
	}
}

func TestFindByName_GIVEN_noMatch_WHEN_searched_THEN_notFound(t *testing.T) {
	worktrees := []gitwt.Worktree{{Path: "/repo/main", Branch: "main"}}

	_, ok := gitwt.FindByName(worktrees, "missing")
	if ok {
		t.Fatalf("expected not found")
	}
}

func TestBranchExists_GIVEN_existingBranch_WHEN_checked_THEN_true(t *testing.T) {
	repo := newFixtureRepo(t)

	got, err := gitwt.BranchExists(repo, "main")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !got {
		t.Fatalf("expected main to exist")
	}
}

func TestBranchExists_GIVEN_missingBranch_WHEN_checked_THEN_false(t *testing.T) {
	repo := newFixtureRepo(t)

	got, err := gitwt.BranchExists(repo, "does-not-exist")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if got {
		t.Fatalf("expected does-not-exist to be absent")
	}
}

func TestCreateListRemove_GIVEN_newBranch_WHEN_createThenListThenRemove_THEN_roundTripsCleanly(t *testing.T) {
	repo := newFixtureRepo(t)
	wtDir := repo + "-worktrees"
	target := wtDir + "/feature"

	if err := gitwt.Create(repo, target, "feature"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	exists, err := gitwt.BranchExists(repo, "feature")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !exists {
		t.Fatalf("expected feature branch to have been created")
	}

	worktrees, err := gitwt.List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("got %d worktrees, want 2: %#v", len(worktrees), worktrees)
	}

	wt, ok := gitwt.FindByName(worktrees, "feature")
	if !ok {
		t.Fatalf("expected to find feature worktree in list")
	}
	if wt.Branch != "feature" {
		t.Fatalf("got branch %q, want feature", wt.Branch)
	}

	if err := gitwt.Remove(repo, wt.Path, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	worktrees, err = gitwt.List(repo)
	if err != nil {
		t.Fatalf("List after remove: %v", err)
	}
	if _, ok := gitwt.FindByName(worktrees, "feature"); ok {
		t.Fatalf("expected feature worktree to be gone after Remove")
	}

	exists, err = gitwt.BranchExists(repo, "feature")
	if err != nil {
		t.Fatalf("BranchExists after remove: %v", err)
	}
	if !exists {
		t.Fatalf("expected feature branch to survive worktree removal")
	}
}

func TestCreate_GIVEN_existingBranch_WHEN_created_THEN_reusesBranchInsteadOfFailing(t *testing.T) {
	repo := newFixtureRepo(t)
	runGit(t, repo, "branch", "existing")
	wtDir := repo + "-worktrees"
	target := wtDir + "/existing"

	if err := gitwt.Create(repo, target, "existing"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	worktrees, err := gitwt.List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	wt, ok := gitwt.FindByName(worktrees, "existing")
	if !ok {
		t.Fatalf("expected existing worktree in list")
	}
	if wt.Branch != "existing" {
		t.Fatalf("got branch %q, want existing", wt.Branch)
	}
}

func TestCreate_GIVEN_branchExistsOnlyOnRemote_WHEN_created_THEN_tracksRemoteBranch(t *testing.T) {
	source := newFixtureRepo(t)
	runGit(t, source, "checkout", "-b", "feat")
	featFile := filepath.Join(source, "feat.txt")
	if err := os.WriteFile(featFile, []byte("feat\n"), 0o644); err != nil {
		t.Fatalf("write feat.txt: %v", err)
	}
	runGit(t, source, "add", "feat.txt")
	runGit(t, source, "commit", "-m", "feat commit")
	runGit(t, source, "checkout", "main")

	bareOrigin := source + ".git"
	runGit(t, filepath.Dir(source), "clone", "--bare", source, bareOrigin)

	repo := source + "-clone"
	runGit(t, filepath.Dir(source), "clone", bareOrigin, repo)
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")

	// A plain clone only checks out main locally; feat exists only as the
	// remote-tracking ref origin/feat, not as a local branch.
	if exists, err := gitwt.BranchExists(repo, "feat"); err != nil || exists {
		t.Fatalf("expected feat to not exist locally after clone, got exists=%v err=%v", exists, err)
	}

	wtDir := repo + "-worktrees"
	target := wtDir + "/feat"

	if err := gitwt.Create(repo, target, "feat"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	wantHash := runGitOutput(t, bareOrigin, "rev-parse", "refs/heads/feat")
	gotHash := runGitOutput(t, target, "rev-parse", "HEAD")
	if gotHash != wantHash {
		t.Fatalf("got worktree HEAD %q, want %q (origin/feat)", gotHash, wantHash)
	}

	upstream := runGitOutput(t, target, "rev-parse", "--abbrev-ref", "feat@{upstream}")
	if upstream != "origin/feat" {
		t.Fatalf("got upstream %q, want origin/feat", upstream)
	}
}

func TestPrune_GIVEN_worktreeDirManuallyDeleted_WHEN_pruned_THEN_metadataRemoved(t *testing.T) {
	repo := newFixtureRepo(t)
	wtDir := repo + "-worktrees"
	target := wtDir + "/feature"

	if err := gitwt.Create(repo, target, "feature"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if err := gitwt.Prune(repo); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	worktrees, err := gitwt.List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, ok := gitwt.FindByName(worktrees, "feature"); ok {
		t.Fatalf("expected pruned worktree metadata to be gone from git worktree list")
	}
}

func TestExec_GIVEN_commandExitingNonZero_WHEN_run_THEN_exitCodePropagated(t *testing.T) {
	repo := newFixtureRepo(t)

	code, err := gitwt.Exec(repo, []string{"sh", "-c", "exit 7"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if code != 7 {
		t.Fatalf("got exit code %d, want 7", code)
	}
}

func TestExec_GIVEN_successfulCommand_WHEN_run_THEN_zeroExitCode(t *testing.T) {
	repo := newFixtureRepo(t)

	code, err := gitwt.Exec(repo, []string{"true"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
}
