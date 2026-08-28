package fsops_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/fsops"
)

func TestCopy_GIVEN_singleFile_WHEN_copied_THEN_contentAndModePreserved(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.sh")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dst := filepath.Join(dir, "sub", "dst.sh")

	if err := fsops.Copy(src, dst); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, []byte("#!/bin/sh\necho hi\n")) {
		t.Fatalf("got content %q", got)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("got mode %v, want 0755", info.Mode().Perm())
	}
}

func TestCopy_GIVEN_directoryTree_WHEN_copied_THEN_recursedAndModesPreserved(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "conf")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile b: %v", err)
	}

	dst := filepath.Join(dir, "worktree", "conf")
	if err := fsops.Copy(src, dst); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	for _, rel := range []string{"a.txt", "nested/b.txt"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}

	info, err := os.Stat(filepath.Join(dst, "nested"))
	if err != nil {
		t.Fatalf("Stat nested: %v", err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("got dir mode %v, want 0750", info.Mode().Perm())
	}
}

func TestCopy_GIVEN_missingSrc_WHEN_copied_THEN_warnsAndSkipsWithoutError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "does-not-exist")
	dst := filepath.Join(dir, "dst")

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stderr = w

	copyErr := fsops.Copy(src, dst)

	w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	if copyErr != nil {
		t.Fatalf("Copy: got error %v, want nil", copyErr)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("expected dst not to be created, stat err: %v", statErr)
	}
	if buf.Len() == 0 {
		t.Fatalf("expected a warning on stderr, got none")
	}
}
