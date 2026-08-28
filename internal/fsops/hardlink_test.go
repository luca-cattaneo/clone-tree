package fsops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/fsops"
)

func TestHardlink_GIVEN_singleFile_WHEN_linked_THEN_sharesInode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "vendor", "lib.php")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(src, []byte("<?php\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dst := filepath.Join(dir, "worktree", "vendor", "lib.php")

	if err := fsops.Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink: %v", err)
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		t.Fatalf("Stat src: %v", err)
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("Stat dst: %v", err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Fatalf("expected src and dst to share an inode")
	}
}

func TestHardlink_GIVEN_directoryTree_WHEN_linked_THEN_dirsCreatedFilesLinked(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(filepath.Join(src, "pkg"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "pkg", "a.php"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dst := filepath.Join(dir, "worktree", "vendor")

	if err := fsops.Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink: %v", err)
	}

	srcInfo, err := os.Stat(filepath.Join(src, "pkg", "a.php"))
	if err != nil {
		t.Fatalf("Stat src file: %v", err)
	}
	dstInfo, err := os.Stat(filepath.Join(dst, "pkg", "a.php"))
	if err != nil {
		t.Fatalf("Stat dst file: %v", err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Fatalf("expected linked file to share an inode")
	}
}

func TestHardlink_GIVEN_missingSrc_WHEN_linked_THEN_warnsAndSkipsWithoutError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "does-not-exist")
	dst := filepath.Join(dir, "dst")

	origStderr := os.Stderr
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	if err := fsops.Hardlink(src, dst); err != nil {
		t.Fatalf("Hardlink: got error %v, want nil", err)
	}
	w.Close()

	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("expected dst not to be created, stat err: %v", statErr)
	}
}
