package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/hooks"
)

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func realpath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", path, err)
	}
	return resolved
}

func TestRun_GIVEN_hookScript_WHEN_run_THEN_envAndCwdArePassedThrough(t *testing.T) {
	dir := t.TempDir()
	wtPath := t.TempDir()
	outFile := filepath.Join(dir, "out.txt")
	script := writeScript(t, dir, "post-create.sh", `
env | sort > "$OUT_FILE"
pwd >> "$OUT_FILE"
`)

	env := hooks.Env("feat-x", 3, "feat-x.dev.local", wtPath, map[string]int{"DB_PORT": 3336})
	env["OUT_FILE"] = outFile

	if err := hooks.Run(script, wtPath, env); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	out := string(got)

	for _, want := range []string{
		"CT_NAME=feat-x",
		"CT_SLOT=3",
		"CT_DNS=feat-x.dev.local",
		"CT_WT_PATH=" + wtPath,
		"DB_PORT=3336",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	gotCwd := lines[len(lines)-1]
	if realpath(t, gotCwd) != realpath(t, wtPath) {
		t.Errorf("got cwd %q, want %q", gotCwd, wtPath)
	}
}

func TestRun_GIVEN_emptyPath_WHEN_run_THEN_noop(t *testing.T) {
	if err := hooks.Run("", t.TempDir(), nil); err != nil {
		t.Fatalf("Run: got error %v, want nil", err)
	}
}

func TestRun_GIVEN_missingFile_WHEN_run_THEN_error(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.sh")

	err := hooks.Run(missing, t.TempDir(), nil)
	if err == nil {
		t.Fatal("Run: got nil error, want error for missing hook")
	}
}

func TestRun_GIVEN_scriptExitsNonZero_WHEN_run_THEN_errorNamesHookAndExitCode(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "pre-remove.sh", "exit 7\n")

	err := hooks.Run(script, t.TempDir(), nil)
	if err == nil {
		t.Fatal("Run: got nil error, want error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "pre-remove.sh") {
		t.Errorf("error %q does not name the hook", err.Error())
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("error %q does not include the exit code", err.Error())
	}
}
