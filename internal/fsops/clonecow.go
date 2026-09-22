package fsops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// `--reflink=auto` degrades to a plain copy internally when the filesystem
// doesn't support reflinks.
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

var runCommand = func(cmd *exec.Cmd) error {
	return cmd.Run()
}

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
