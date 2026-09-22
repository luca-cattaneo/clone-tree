package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// errCrossDevice signals that os.Link failed with EXDEV (src and dst live
// on different devices).
var errCrossDevice = errors.New("fsops: cross-device link")

var osLink = os.Link

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
