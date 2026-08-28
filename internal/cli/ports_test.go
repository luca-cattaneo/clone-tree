package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/config"
)

func intPtr(n int) *int { return &n }

func fixturePortsConfig() *config.Config {
	return &config.Config{
		Ports: map[string]config.Port{
			"DB_PORT":    {Base: intPtr(3306), Step: 10},
			"PROXY_PORT": {Base: intPtr(10443), Step: 100},
		},
	}
}

func TestSortedPortVars_GIVEN_multipleVars_WHEN_sorted_THEN_alphabetical(t *testing.T) {
	got := sortedPortVars(fixturePortsConfig().Ports)
	want := []string{"DB_PORT", "PROXY_PORT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestPortsRows_GIVEN_registeredWorktrees_WHEN_built_THEN_mainFirstThenSortedBySlotWithComputedValues(t *testing.T) {
	cfg := fixturePortsConfig()
	vars := sortedPortVars(cfg.Ports)
	slotByName := map[string]int{"zeta": 2, "alpha": 1}

	got := portsRows(slotByName, cfg, vars)

	want := [][]string{
		{"0", "main", "3306", "10443"},
		{"1", "alpha", "3316", "10543"},
		{"2", "zeta", "3326", "10643"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestPortVarValueRows_GIVEN_slot_WHEN_built_THEN_varValuePairs(t *testing.T) {
	cfg := fixturePortsConfig()
	vars := sortedPortVars(cfg.Ports)

	got := portVarValueRows(cfg, 1, vars)

	want := [][]string{
		{"DB_PORT", "3316"},
		{"PROXY_PORT", "10543"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestPortsCmd_GIVEN_fixtureRepoWithPortsAndRegisteredWorktree_WHEN_runWithoutArgs_THEN_noError(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	// Pick a base whose slot-1 value (base+10) lands on a currently-free
	// port, mirroring TestCreate_GIVEN_candidateSlotPortAlreadyBusy's
	// approach: create's busy-port probe would otherwise abort
	// nondeterministically depending on what's already listening on the
	// dev machine.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	freePort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	configYAML := "version: 1\n" +
		"worktrees_dir: ../repo-worktrees\n" +
		"dns_pattern: \"\"\n" +
		"max_slots: 9\n" +
		fmt.Sprintf("ports:\n  DB_PORT: {base: %d, step: 10}\n", freePort-10) +
		"env: {}\n" +
		"files: {}\n" +
		"hooks: {}\n"
	if err := os.MkdirAll(filepath.Join(repoDir, ".clone-tree"), 0o755); err != nil {
		t.Fatalf("MkdirAll .clone-tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".clone-tree", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	chdir(t, repoDir)
	configPath = ""
	createBranch = ""

	if err := createCmd.RunE(createCmd, []string{"feature"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := portsCmd.RunE(portsCmd, nil); err != nil {
		t.Fatalf("ports: %v", err)
	}
	if err := portsCmd.RunE(portsCmd, []string{"feature"}); err != nil {
		t.Fatalf("ports feature: %v", err)
	}
	if err := portsCmd.RunE(portsCmd, []string{"nonexistent"}); err == nil {
		t.Fatalf("expected error for unregistered worktree name")
	}
}
