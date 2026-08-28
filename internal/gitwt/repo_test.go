package gitwt_test

import (
	"path/filepath"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
)

func TestRepoRoot_GIVEN_mainRepoDir_WHEN_resolved_THEN_returnsSameDir(t *testing.T) {
	repo := newFixtureRepo(t)

	got, err := gitwt.RepoRoot(repo)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if gotResolved != want {
		t.Fatalf("got %q, want %q", gotResolved, want)
	}
}

func TestRepoRoot_GIVEN_dirInsideLinkedWorktree_WHEN_resolved_THEN_returnsMainRepoRoot(t *testing.T) {
	repo := newFixtureRepo(t)
	wtPath := repo + "-worktrees/feature"
	if err := gitwt.Create(repo, wtPath, "feature"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := gitwt.RepoRoot(wtPath)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if gotResolved != want {
		t.Fatalf("got %q, want %q (--show-toplevel would have wrongly returned the worktree root)", gotResolved, want)
	}
}

func TestRepoRoot_GIVEN_notAGitRepo_WHEN_resolved_THEN_errors(t *testing.T) {
	dir := t.TempDir()

	if _, err := gitwt.RepoRoot(dir); err == nil {
		t.Fatalf("expected error for non-git directory")
	}
}

func TestRepoName_GIVEN_mainRepoDir_WHEN_resolved_THEN_returnsBasename(t *testing.T) {
	repo := newFixtureRepo(t)

	got, err := gitwt.RepoName(repo)
	if err != nil {
		t.Fatalf("RepoName: %v", err)
	}
	if got != filepath.Base(repo) {
		t.Fatalf("got %q, want %q", got, filepath.Base(repo))
	}
}

func TestWorktreesDir_GIVEN_mainRepoDir_WHEN_resolved_THEN_returnsSiblingDir(t *testing.T) {
	repo := newFixtureRepo(t)

	got, err := gitwt.WorktreesDir(repo)
	if err != nil {
		t.Fatalf("WorktreesDir: %v", err)
	}

	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(filepath.Dir(resolvedRepo), filepath.Base(resolvedRepo)+"-worktrees")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
