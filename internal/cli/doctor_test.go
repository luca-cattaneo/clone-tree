package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
)

func runDoctorCmd(t *testing.T) (string, error) {
	t.Helper()

	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("Pipe: %v", pipeErr)
	}
	orig := os.Stdout
	os.Stdout = w
	err := doctorCmd.RunE(doctorCmd, nil)
	_ = w.Close()
	os.Stdout = orig

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("ReadAll: %v", readErr)
	}
	return string(out), err
}

func writeMinimalConfig(t *testing.T, repoDir, extra string) {
	t.Helper()
	yaml := "version: 1\n" +
		"base_branch: main\n" +
		"dns_pattern: \"\"\n" +
		"max_slots: 9\n" +
		"ports: {}\n" +
		"env: {}\n" +
		"files: {}\n" +
		"hooks: {}\n" +
		extra
	if err := os.MkdirAll(filepath.Join(repoDir, ".clone-tree"), 0o755); err != nil {
		t.Fatalf("MkdirAll .clone-tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".clone-tree", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

func TestDoctorCmd_GIVEN_noConfigInRepo_WHEN_run_THEN_bareModeWarningAndNoFailure(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)
	chdir(t, repoDir)
	configPath = ""

	out, err := runDoctorCmd(t)

	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(out, "⚠ no config — bare mode; run ct create-config") {
		t.Fatalf("expected bare-mode warning, got:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(repoDir, ".clone-tree")); !os.IsNotExist(statErr) {
		t.Fatalf("expected doctor to never scaffold .clone-tree/, stat err: %v", statErr)
	}
}

func TestDoctorCmd_GIVEN_healthyRegisteredWorktree_WHEN_run_THEN_allChecksPass(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)
	writeMinimalConfig(t, repoDir, "")

	chdir(t, repoDir)
	configPath = ""
	createBranch = ""

	if err := createCmd.RunE(createCmd, []string{"feature"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := runDoctorCmd(t)

	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("expected no failing checks, got:\n%s", out)
	}
}

func TestDoctorCmd_GIVEN_orphanRegisteredSlot_WHEN_run_THEN_orphanSlotFails(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)
	writeMinimalConfig(t, repoDir, "")

	chdir(t, repoDir)
	configPath = ""

	reg, err := slots.Load(repoDir)
	if err != nil {
		t.Fatalf("slots.Load: %v", err)
	}
	if _, err := reg.Allocate("stale", 9); err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	out, err := runDoctorCmd(t)

	if !errors.Is(err, ErrChecksFailed) {
		t.Fatalf("expected ErrChecksFailed, got %v", err)
	}
	if !strings.Contains(out, "orphan slot 1 (stale) — run ct remove stale") {
		t.Fatalf("expected orphan slot line, got:\n%s", out)
	}
}

func TestDoctorCmd_GIVEN_unregisteredCtNamedWorktree_WHEN_run_THEN_warnsUnregisteredWorktree(t *testing.T) {
	projectsDir, repoDir := newCreateFixtureRepo(t)
	writeMinimalConfig(t, repoDir, "")

	rogue := filepath.Join(projectsDir, "repo-rogue")
	if err := gitwt.Create(repoDir, rogue, "rogue", "main"); err != nil {
		t.Fatalf("gitwt.Create: %v", err)
	}

	chdir(t, repoDir)
	configPath = ""

	out, _ := runDoctorCmd(t)

	if !strings.Contains(out, "⚠ unregistered worktree "+rogue) {
		t.Fatalf("expected unregistered worktree line for %s, got:\n%s", rogue, out)
	}
}

// dbPortFixture registers "feature" at slot 1 while its DB_PORT is still
// free — create's own busy-port probe would otherwise abort the create.
func dbPortFixture(t *testing.T) (repoDir string, slot1Port int) {
	t.Helper()
	_, repoDir = newCreateFixtureRepo(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	slot1Port = ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	yaml := "version: 1\n" +
		"base_branch: main\n" +
		"dns_pattern: \"\"\n" +
		"max_slots: 9\n" +
		fmt.Sprintf("ports:\n  DB_PORT: {base: %d, step: 10}\n", slot1Port-10) +
		"env: {}\n" +
		"files: {}\n" +
		"hooks: {}\n"
	if err := os.MkdirAll(filepath.Join(repoDir, ".clone-tree"), 0o755); err != nil {
		t.Fatalf("MkdirAll .clone-tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".clone-tree", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	chdir(t, repoDir)
	configPath = ""
	createBranch = ""
	if err := createCmd.RunE(createCmd, []string{"feature"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	return repoDir, slot1Port
}

func stubComposeRunner(t *testing.T, fn func(dir, project string, args ...string) ([]byte, error)) {
	t.Helper()
	orig := composeRunner
	composeRunner = fn
	t.Cleanup(func() { composeRunner = orig })
}

func TestDoctorCmd_GIVEN_stackNotRunningAndPortBusy_WHEN_run_THEN_busyPortFails(t *testing.T) {
	_, slot1Port := dbPortFixture(t)

	stubComposeRunner(t, func(_, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", slot1Port))
	if err != nil {
		t.Fatalf("re-Listen on %d: %v", slot1Port, err)
	}
	defer ln.Close()

	out, err := runDoctorCmd(t)

	if !errors.Is(err, ErrChecksFailed) {
		t.Fatalf("expected ErrChecksFailed, got %v", err)
	}
	if !strings.Contains(out, "✗ feature (slot 1): busy ports:") || !strings.Contains(out, "DB_PORT") {
		t.Fatalf("expected a busy DB_PORT ✗ line for feature, got:\n%s", out)
	}
}

func TestDoctorCmd_GIVEN_stackRunning_WHEN_run_THEN_informationalOKAndPortsNotProbed(t *testing.T) {
	_, slot1Port := dbPortFixture(t)

	stubComposeRunner(t, func(_, _ string, _ ...string) ([]byte, error) {
		return []byte("abc123\ndef456\n"), nil
	})

	// Bind the exact port anyway: proves a running stack short-circuits to
	// the informational ✓ without probing ports at all.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", slot1Port))
	if err != nil {
		t.Fatalf("re-Listen on %d: %v", slot1Port, err)
	}
	defer ln.Close()

	out, err := runDoctorCmd(t)

	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(out, "✓ feature (slot 1): stack running (2 containers)") {
		t.Fatalf("expected the informational stack-running line, got:\n%s", out)
	}
	if strings.Contains(out, "DB_PORT") {
		t.Fatalf("expected ports NOT to be probed when the stack is running, got:\n%s", out)
	}
}

func TestDoctorCmd_GIVEN_dockerUnavailableAndPortBusy_WHEN_run_THEN_busyPortWarnsNotFails(t *testing.T) {
	_, slot1Port := dbPortFixture(t)

	stubComposeRunner(t, func(_, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("docker: command not found")
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", slot1Port))
	if err != nil {
		t.Fatalf("re-Listen on %d: %v", slot1Port, err)
	}
	defer ln.Close()

	out, err := runDoctorCmd(t)

	if err != nil {
		t.Fatalf("doctor: %v (expected a docker-unavailable busy port to warn, not fail)", err)
	}
	if !strings.Contains(out, "⚠ feature (slot 1): busy ports:") || !strings.Contains(out, "DB_PORT") {
		t.Fatalf("expected a busy DB_PORT ⚠ line for feature, got:\n%s", out)
	}
}

func TestDoctorCmd_GIVEN_orphanComposeProject_WHEN_run_THEN_orphanStackFails(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)
	writeMinimalConfig(t, repoDir, "")

	chdir(t, repoDir)
	configPath = ""
	createBranch = ""

	if err := createCmd.RunE(createCmd, []string{"feature"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// repo-feature: registered worktree, must not be flagged. repo: bare
	// main project, must not be flagged. repo-stale: no registered worktree
	// behind it — the orphan. other-thing: outside repo's project prefix
	// entirely, not this repo's concern.
	stubComposeRunner(t, func(_, _ string, _ ...string) ([]byte, error) {
		return []byte(`[{"Name":"repo-feature"},{"Name":"repo"},{"Name":"repo-stale"},{"Name":"other-thing"}]`), nil
	})

	out, err := runDoctorCmd(t)

	if !errors.Is(err, ErrChecksFailed) {
		t.Fatalf("expected ErrChecksFailed, got %v", err)
	}
	if !strings.Contains(out, "✗ orphan compose project repo-stale — docker compose -p repo-stale down -v --remove-orphans") {
		t.Fatalf("expected orphan stack line for repo-stale, got:\n%s", out)
	}
	if strings.Count(out, "orphan compose project") != 1 {
		t.Fatalf("expected exactly one orphan compose project finding, got:\n%s", out)
	}
}

func TestDoctorCmd_GIVEN_dockerUnavailable_WHEN_run_THEN_orphanStacksWarnsNotFails(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)
	writeMinimalConfig(t, repoDir, "")

	chdir(t, repoDir)
	configPath = ""

	stubComposeRunner(t, func(_, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("docker: command not found")
	})

	out, err := runDoctorCmd(t)

	if err != nil {
		t.Fatalf("doctor: %v (expected a docker-unavailable orphan-stacks check to warn, not fail)", err)
	}
	if !strings.Contains(out, "⚠ docker compose ls unavailable — skipped") {
		t.Fatalf("expected orphan-stacks docker-unavailable warning, got:\n%s", out)
	}
}

func TestDoctorCmd_GIVEN_missingPostCreateHook_WHEN_run_THEN_hookCheckFails(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)
	yaml := "version: 1\n" +
		"base_branch: main\n" +
		"dns_pattern: \"\"\n" +
		"max_slots: 9\n" +
		"ports: {}\n" +
		"env: {}\n" +
		"files: {}\n" +
		"hooks:\n  post_create: .clone-tree/hooks/post-create.sh\n"
	if err := os.MkdirAll(filepath.Join(repoDir, ".clone-tree"), 0o755); err != nil {
		t.Fatalf("MkdirAll .clone-tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".clone-tree", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	chdir(t, repoDir)
	configPath = ""

	out, err := runDoctorCmd(t)

	if !errors.Is(err, ErrChecksFailed) {
		t.Fatalf("expected ErrChecksFailed, got %v", err)
	}
	if !strings.Contains(out, "✗ post_create hook not found:") {
		t.Fatalf("expected missing hook line, got:\n%s", out)
	}
}
