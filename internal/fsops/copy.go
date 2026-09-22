package fsops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func Copy(src, dst string) error {
	info, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		warnMissing(src)
		return nil
	}
	if err != nil {
		return fmt.Errorf("fsops: stat %s: %w", src, err)
	}
	return copyPath(src, dst, info)
}

func copyPath(src, dst string, info os.FileInfo) error {
	if info.IsDir() {
		return copyDir(src, dst, info)
	}
	return copyFile(src, dst, info)
}

func copyDir(src, dst string, info os.FileInfo) error {
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("fsops: mkdir %s: %w", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("fsops: readdir %s: %w", src, err)
	}
	for _, entry := range entries {
		childInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("fsops: stat %s: %w", filepath.Join(src, entry.Name()), err)
		}
		if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), childInfo); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("fsops: mkdir %s: %w", filepath.Dir(dst), err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("fsops: open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("fsops: create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("fsops: copy %s -> %s: %w", src, dst, err)
	}
	// Explicit chmod: OpenFile's perm argument is masked by umask on
	// creation, so an executable source file could otherwise lose its
	// executable bit in the copy.
	if err := out.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("fsops: chmod %s: %w", dst, err)
	}
	return nil
}
