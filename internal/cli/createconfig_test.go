package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateConfigCmd_GIVEN_noConfigInRepo_WHEN_run_THEN_scaffoldsAndPrintsNoticeThenExitsClean(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	chdir(t, repoDir)
	configPath = ""

	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("Pipe: %v", pipeErr)
	}
	origStdout := os.Stdout
	os.Stdout = w
	err := createConfigCmd.RunE(createConfigCmd, nil)
	_ = w.Close()
	os.Stdout = origStdout
	if err != nil {
		t.Fatalf("create-config: %v", err)
	}

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("ReadAll: %v", readErr)
	}
	got := string(out)
	for _, want := range []string{"Generated", "stopped here on purpose", "commit .clone-tree/config.yaml"} {
		if !strings.Contains(got, want) {
			t.Fatalf("got stdout %q, want it to contain %q", got, want)
		}
	}

	wantPath := filepath.Join(repoDir, ".clone-tree", "config.yaml")
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Fatalf("expected scaffold to write config.yaml: %v", statErr)
	}
}

func TestCreateConfigCmd_GIVEN_configAlreadyExists_WHEN_runAgain_THEN_errorsWithoutOverwriting(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	chdir(t, repoDir)
	configPath = ""

	if err := createConfigCmd.RunE(createConfigCmd, nil); err != nil {
		t.Fatalf("first create-config: %v", err)
	}

	wantPath := filepath.Join(repoDir, ".clone-tree", "config.yaml")
	before, readErr := os.ReadFile(wantPath)
	if readErr != nil {
		t.Fatalf("ReadFile before second run: %v", readErr)
	}

	err := createConfigCmd.RunE(createConfigCmd, nil)
	if err == nil {
		t.Fatalf("expected create-config to error when a config already exists")
	}
	want := "config already exists: " + wantPath
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}

	after, readErr := os.ReadFile(wantPath)
	if readErr != nil {
		t.Fatalf("ReadFile after second run: %v", readErr)
	}
	if string(before) != string(after) {
		t.Fatalf("expected the existing config.yaml to be untouched by the second run")
	}
}
