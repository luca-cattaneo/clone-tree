// Package gitwt wraps git worktree operations and main-repo path resolution.
package gitwt

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoRoot resolves the main repository root directory, even when dir is
// inside a linked worktree. It relies on --git-common-dir, which always
// points at the main repo's .git directory regardless of which worktree
// git is invoked from.
func RepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("not a git repository: %s", strings.TrimSpace(stderr.String()))
	}
	commonDir := strings.TrimSpace(stdout.String())
	return filepath.Dir(commonDir), nil
}

// RepoName returns the basename of the main repo root.
func RepoName(dir string) (string, error) {
	root, err := RepoRoot(dir)
	if err != nil {
		return "", err
	}
	return filepath.Base(root), nil
}

// WorktreesDir returns the sibling worktrees directory for the main repo:
// <parent-of-repo>/<repo>-worktrees.
func WorktreesDir(dir string) (string, error) {
	root, err := RepoRoot(dir)
	if err != nil {
		return "", err
	}
	name := filepath.Base(root)
	parent := filepath.Dir(root)
	return filepath.Join(parent, name+"-worktrees"), nil
}
