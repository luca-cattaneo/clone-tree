package fsops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/fsops"
)

func TestSymlinkSiblings_GIVEN_noExistingLink_WHEN_created_THEN_pointsAtProjectsDirSibling(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	worktreesDir := filepath.Join(dir, "repo-worktrees")
	if err := os.MkdirAll(filepath.Join(projectsDir, "sibling"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := fsops.SymlinkSiblings([]string{"sibling"}, projectsDir, worktreesDir); err != nil {
		t.Fatalf("SymlinkSiblings: %v", err)
	}

	got, err := os.Readlink(filepath.Join(worktreesDir, "sibling"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if want := "../sibling"; got != want {
		t.Fatalf("got link target %q, want %q", got, want)
	}
}

func TestSymlinkSiblings_GIVEN_alreadyCorrectSymlink_WHEN_reRun_THEN_noop(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	worktreesDir := filepath.Join(dir, "repo-worktrees")
	if err := os.MkdirAll(filepath.Join(projectsDir, "sibling"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := fsops.SymlinkSiblings([]string{"sibling"}, projectsDir, worktreesDir); err != nil {
		t.Fatalf("first SymlinkSiblings: %v", err)
	}
	if err := fsops.SymlinkSiblings([]string{"sibling"}, projectsDir, worktreesDir); err != nil {
		t.Fatalf("second SymlinkSiblings: %v", err)
	}

	got, err := os.Readlink(filepath.Join(worktreesDir, "sibling"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if want := "../sibling"; got != want {
		t.Fatalf("got link target %q, want %q", got, want)
	}
}

func TestSymlinkSiblings_GIVEN_pathExistsAsRegularFile_WHEN_created_THEN_noErrorAndUntouched(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	worktreesDir := filepath.Join(dir, "repo-worktrees")
	if err := os.MkdirAll(filepath.Join(projectsDir, "sibling"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll worktreesDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreesDir, "sibling"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := fsops.SymlinkSiblings([]string{"sibling"}, projectsDir, worktreesDir); err != nil {
		t.Fatalf("SymlinkSiblings: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(worktreesDir, "sibling"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "x" {
		t.Fatalf("expected the regular file to be left untouched, got %q", got)
	}
}

func TestSymlinkSiblings_GIVEN_symlinkToWrongTarget_WHEN_created_THEN_noErrorAndUntouched(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	worktreesDir := filepath.Join(dir, "repo-worktrees")
	if err := os.MkdirAll(filepath.Join(projectsDir, "sibling"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll worktreesDir: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(worktreesDir, "sibling")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := fsops.SymlinkSiblings([]string{"sibling"}, projectsDir, worktreesDir); err != nil {
		t.Fatalf("SymlinkSiblings: %v", err)
	}

	got, err := os.Readlink(filepath.Join(worktreesDir, "sibling"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if want := filepath.Join(dir, "elsewhere"); got != want {
		t.Fatalf("expected the existing symlink to be left untouched, got %q, want %q", got, want)
	}
}

func TestSymlinkSiblings_GIVEN_sourceMissing_WHEN_created_THEN_noLinkCreatedNilError(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	worktreesDir := filepath.Join(dir, "repo-worktrees")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := fsops.SymlinkSiblings([]string{"sibling"}, projectsDir, worktreesDir); err != nil {
		t.Fatalf("SymlinkSiblings: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(worktreesDir, "sibling")); !os.IsNotExist(err) {
		t.Fatalf("expected no link to be created, Lstat err: %v", err)
	}
}
