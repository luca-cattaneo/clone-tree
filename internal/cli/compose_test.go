package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunCompose_GIVEN_noConfig_WHEN_started_THEN_errorsWithBareModeMessage(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	chdir(t, repoDir)
	configPath = ""

	err := runCompose("feature", "up", "-d")
	if err == nil {
		t.Fatalf("expected an error when no .clone-tree config exists")
	}
	if err.Error() != "start/stop require a .clone-tree config" {
		t.Fatalf("got error %q, want %q", err.Error(), "start/stop require a .clone-tree config")
	}
}

func TestComposeProjectName_GIVEN_repoAndName_WHEN_built_THEN_hyphenJoined(t *testing.T) {
	got := composeProjectName("myrepo", "feature")
	if want := "myrepo-feature"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestComposeProjectName_GIVEN_mixedCaseRepoOrName_WHEN_built_THEN_lowercased(t *testing.T) {
	got := composeProjectName("MyApp", "documentCache")
	if want := "myapp-documentcache"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunningContainers_GIVEN_composeRunnerErrors_WHEN_counted_THEN_dashPlaceholder(t *testing.T) {
	orig := composeRunner
	defer func() { composeRunner = orig }()
	composeRunner = func(_, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("docker: command not found")
	}

	if got := runningContainers("/some/dir", "repo-feature"); got != "-" {
		t.Fatalf("got %q, want -", got)
	}
}

func TestRunningContainers_GIVEN_composeRunnerReturnsIds_WHEN_counted_THEN_lineCount(t *testing.T) {
	orig := composeRunner
	defer func() { composeRunner = orig }()
	composeRunner = func(_, _ string, _ ...string) ([]byte, error) {
		return []byte("abc123\ndef456\n"), nil
	}

	if got := runningContainers("/some/dir", "repo-feature"); got != "2" {
		t.Fatalf("got %q, want 2", got)
	}
}

func TestComposeDown_GIVEN_composeFilePresent_WHEN_down_THEN_argsIncludeRemoveOrphans(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose.yaml: %v", err)
	}

	var gotArgs []string
	stubComposeRunner(t, func(_, _ string, args ...string) ([]byte, error) {
		gotArgs = args
		return nil, nil
	})

	if err := composeDown(dir, "repo-feature"); err != nil {
		t.Fatalf("composeDown: %v", err)
	}

	want := []string{"down", "-v", "--remove-orphans"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("got args %v, want %v", gotArgs, want)
	}
}

func TestRunningContainers_GIVEN_composeRunnerReturnsEmptyOutput_WHEN_counted_THEN_zero(t *testing.T) {
	orig := composeRunner
	defer func() { composeRunner = orig }()
	composeRunner = func(_, _ string, _ ...string) ([]byte, error) {
		return []byte("\n"), nil
	}

	if got := runningContainers("/some/dir", "repo-feature"); got != "0" {
		t.Fatalf("got %q, want 0", got)
	}
}
