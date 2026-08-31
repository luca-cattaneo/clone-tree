package cli

import (
	"errors"
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
	got := composeProjectName("TagPay", "documentCache")
	if want := "tagpay-documentcache"; got != want {
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
