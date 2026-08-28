package fsops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// cloneCoWCommand returns the platform-specific copy-on-write reflink
// clone command for src -> dst, or nil on a platform with no such command.
// `cp -Rc` (darwin/APFS) and `cp -R --reflink=auto` (linux/btrfs,xfs) both
// share blocks with the source instead of duplicating them, so cloning a
// multi-GB datadir takes seconds instead of minutes; `--reflink=auto`
// degrades to a plain copy internally when the filesystem doesn't support
// reflinks.
func cloneCoWCommand(src, dst string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("cp", "-Rc", src, dst)
	case "linux":
		return exec.Command("cp", "-R", "--reflink=auto", src, dst)
	default:
		return nil
	}
}

// runCommand executes cmd. Package-level so tests can substitute a failing
// implementation and exercise the plain-Copy fallback without needing a
// real non-reflink-capable filesystem.
var runCommand = func(cmd *exec.Cmd) error {
	return cmd.Run()
}

// CloneCoW clones src to dst using the platform's copy-on-write reflink
// command, falling back to a plain recursive Copy when the platform has no
// such command or the command fails (e.g. filesystem without reflink
// support). dst must not already exist — CloneCoW never clobbers existing
// data.
func CloneCoW(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("fsops: clone_cow destination already exists: %s", dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("fsops: stat %s: %w", dst, err)
	}

	if cmd := cloneCoWCommand(src, dst); cmd != nil {
		if err := runCommand(cmd); err == nil {
			return nil
		}
		// Clean up whatever the failed command attempt left behind before
		// falling back, so Copy starts from a clean slate.
		_ = os.RemoveAll(dst)
	}

	return Copy(src, dst)
}
