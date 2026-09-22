package fsops

import (
	"fmt"
	"os"
	"path/filepath"
)

func SymlinkSiblings(names []string, projectsDir, worktreesDir string) error {
	for _, name := range names {
		if err := symlinkSibling(name, projectsDir, worktreesDir); err != nil {
			return err
		}
	}
	return nil
}

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
