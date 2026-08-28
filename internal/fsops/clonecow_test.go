package fsops

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// buildTree writes a small nested file tree under root for CloneCoW tests.
func buildTree(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile b: %v", err)
	}
}

func assertTreeMatches(t *testing.T, dst string) {
	t.Helper()
	for rel, want := range map[string]string{"a.txt": "a", "nested/b.txt": "b"} {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s: got %q, want %q", rel, got, want)
		}
	}
}

func TestCloneCoW_GIVEN_platformCommandSucceeds_WHEN_cloned_THEN_identicalTree(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "datadir")
	buildTree(t, src)
	dst := filepath.Join(dir, "datadir_feature")

	if err := CloneCoW(src, dst); err != nil {
		t.Fatalf("CloneCoW: %v", err)
	}
	assertTreeMatches(t, dst)
}

func TestCloneCoW_GIVEN_platformCommandFails_WHEN_cloned_THEN_fallsBackToPlainCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "datadir")
	buildTree(t, src)
	dst := filepath.Join(dir, "datadir_feature")

	orig := runCommand
	defer func() { runCommand = orig }()
	called := false
	runCommand = func(_ *exec.Cmd) error {
		called = true
		return errors.New("simulated: reflink not supported on this filesystem")
	}

	if err := CloneCoW(src, dst); err != nil {
		t.Fatalf("CloneCoW: %v", err)
	}
	if !called {
		t.Fatalf("expected the platform command to have been attempted")
	}
	assertTreeMatches(t, dst)
}

func TestCloneCoW_GIVEN_dstAlreadyExists_WHEN_cloned_THEN_errorsWithoutClobbering(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "datadir")
	buildTree(t, src)
	dst := filepath.Join(dir, "datadir_feature")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := CloneCoW(src, dst); err == nil {
		t.Fatalf("expected error when dst already exists")
	}

	got, err := os.ReadFile(filepath.Join(dst, "keep.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "mine" {
		t.Fatalf("existing dst content clobbered: got %q", got)
	}
}
