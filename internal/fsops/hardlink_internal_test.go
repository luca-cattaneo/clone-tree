package fsops

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestHardlink_GIVEN_crossDeviceLinkError_WHEN_linked_THEN_fallsBackToSymlinkingWholeSrcDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(filepath.Join(src, "pkg"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "pkg", "a.php"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dst := filepath.Join(dir, "worktree", "vendor")

	orig := osLink
	osLink = func(string, string) error {
		return &os.LinkError{Op: "link", Err: syscall.EXDEV}
	}
	defer func() { osLink = orig }()

	if err := Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink: %v", err)
	}

	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("Lstat dst: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected dst to be a symlink, got mode %v", info.Mode())
	}
	target, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != src {
		t.Fatalf("got symlink target %q, want %q", target, src)
	}
}

func TestHardlink_GIVEN_crossDeviceErrorAfterPartialDirCreated_WHEN_linked_THEN_partialTreeDiscarded(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.php"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dst := filepath.Join(dir, "worktree", "vendor")

	orig := osLink
	osLink = func(string, string) error {
		return &os.LinkError{Op: "link", Err: syscall.EXDEV}
	}
	defer func() { osLink = orig }()

	if err := Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink: %v", err)
	}

	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("Lstat dst: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected dst to end up a symlink (partial real dir discarded), got mode %v", info.Mode())
	}
	if info.IsDir() {
		t.Fatalf("expected dst to no longer be a real directory")
	}
}
