package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// errCrossDevice signals that src and dst live on different devices
// (os.Link failed with EXDEV) partway through a recursive hardlink walk.
// It is never wrapped, so errors.Is sees it all the way up through
// hardlinkDir/hardlinkPath to Hardlink, which then discards whatever
// partial dst tree was built and symlinks the whole src dir instead.
var errCrossDevice = errors.New("fsops: cross-device link")

// osLink is os.Link, indirected so tests can inject an EXDEV-shaped error
// without needing two real devices.
var osLink = os.Link

// Hardlink links src into dst, recursively when src is a directory:
// directories are created fresh (hardlinking a directory isn't portable),
// regular files are hardlinked with os.Link. When src and dst live on
// different devices, os.Link fails with EXDEV — clone-tree then discards
// any partial dst tree and symlinks the whole src dir instead (a copy of a
// large gitignored tree, e.g. vendor/, across devices would defeat the
// point of hardlinking it in the first place). A missing src is not an
// error (see warnMissing).
func Hardlink(src, dst string) error {
	info, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		warnMissing(src)
		return nil
	}
	if err != nil {
		return fmt.Errorf("fsops: stat %s: %w", src, err)
	}

	if err := hardlinkPath(src, dst, info); err != nil {
		if errors.Is(err, errCrossDevice) {
			_ = os.RemoveAll(dst)
			return symlinkWholeDir(src, dst)
		}
		return err
	}
	return nil
}

func symlinkWholeDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("fsops: mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.Symlink(src, dst); err != nil {
		return fmt.Errorf("fsops: symlink %s -> %s: %w", dst, src, err)
	}
	return nil
}

func hardlinkPath(src, dst string, info os.FileInfo) error {
	if info.IsDir() {
		return hardlinkDir(src, dst, info)
	}
	return hardlinkFile(src, dst, info)
}

func hardlinkDir(src, dst string, info os.FileInfo) error {
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
		if err := hardlinkPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), childInfo); err != nil {
			return err
		}
	}
	return nil
}

func hardlinkFile(src, dst string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("fsops: mkdir %s: %w", filepath.Dir(dst), err)
	}

	err := osLink(src, dst)
	if err == nil {
		return nil
	}

	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && errors.Is(linkErr.Err, syscall.EXDEV) {
		return errCrossDevice
	}
	return fmt.Errorf("fsops: link %s -> %s: %w", src, dst, err)
}
