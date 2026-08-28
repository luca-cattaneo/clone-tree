package fsops

import (
	"fmt"
	"os"
	"path/filepath"
)

// SymlinkSiblings ensures <worktreesDir>/<name> is a symlink to
// <projectsDir>/<name>, for every name whose source exists under
// projectsDir and that doesn't already have something at the link path.
// These links are shared by every worktree under worktreesDir (relative
// `../sibling` mounts in compose files resolve the same way from any
// worktree) — creating them is idempotent so every `ct create` can re-run
// it safely, and `ct remove` must never remove them, since doing so would
// break every other worktree's sibling mount.
func SymlinkSiblings(names []string, projectsDir, worktreesDir string) error {
	for _, name := range names {
		if err := symlinkSibling(name, projectsDir, worktreesDir); err != nil {
			return err
		}
	}
	return nil
}

// symlinkSibling mimics `[[ -e "$PROJECTS_DIR/$sibling" && ! -e
// "$WORKTREES_DIR/$sibling" ]] && ln -s "../$sibling" "$WORKTREES_DIR/$sibling"`:
// a missing source or a pre-existing link (of any kind) is silently skipped,
// never checked or compared — only a genuine mkdir/symlink failure errors.
func symlinkSibling(name, projectsDir, worktreesDir string) error {
	if _, err := os.Stat(filepath.Join(projectsDir, name)); err != nil {
		return nil
	}

	link := filepath.Join(worktreesDir, name)
	if _, err := os.Lstat(link); err == nil {
		return nil
	}

	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		return fmt.Errorf("fsops: mkdir %s: %w", worktreesDir, err)
	}
	target := "../" + name
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("fsops: symlink %s -> %s: %w", link, target, err)
	}
	return nil
}
