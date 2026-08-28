package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/hosts"
)

func TestHostsRow_GIVEN_nameRegisteredInHostsFile_WHEN_built_THEN_checkmarkColumn(t *testing.T) {
	hostsFile := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(hostsFile, nil, 0o644); err != nil {
		t.Fatalf("write hostsFile: %v", err)
	}
	origHostsPath := hostsPath
	hostsPath = hostsFile
	t.Cleanup(func() { hostsPath = origHostsPath })

	cfg := &config.Config{DNSPattern: "local-{name}.dev.test"}

	if err := hosts.Add(hostsFile, "feature", "local-feature.dev.test"); err != nil {
		t.Fatalf("hosts.Add: %v", err)
	}

	got, err := hostsRow(cfg, "feature", 1)
	if err != nil {
		t.Fatalf("hostsRow: %v", err)
	}
	want := []string{"1", "feature", "local-feature.dev.test", "✓"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestHostsRow_GIVEN_nameAbsentFromHostsFile_WHEN_built_THEN_dashColumn(t *testing.T) {
	hostsFile := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(hostsFile, nil, 0o644); err != nil {
		t.Fatalf("write hostsFile: %v", err)
	}
	origHostsPath := hostsPath
	hostsPath = hostsFile
	t.Cleanup(func() { hostsPath = origHostsPath })

	cfg := &config.Config{DNSPattern: "local-{name}.dev.test"}

	got, err := hostsRow(cfg, "feature", 1)
	if err != nil {
		t.Fatalf("hostsRow: %v", err)
	}
	want := []string{"1", "feature", "local-feature.dev.test", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestHostsRows_GIVEN_multipleRegistered_WHEN_built_THEN_sortedBySlot(t *testing.T) {
	hostsFile := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(hostsFile, nil, 0o644); err != nil {
		t.Fatalf("write hostsFile: %v", err)
	}
	origHostsPath := hostsPath
	hostsPath = hostsFile
	t.Cleanup(func() { hostsPath = origHostsPath })

	cfg := &config.Config{DNSPattern: "local-{name}.dev.test"}
	slotByName := map[string]int{"zeta": 2, "alpha": 1}

	got, err := hostsRows(slotByName, cfg)
	if err != nil {
		t.Fatalf("hostsRows: %v", err)
	}
	want := [][]string{
		{"1", "alpha", "local-alpha.dev.test", "-"},
		{"2", "zeta", "local-zeta.dev.test", "-"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestHostsCmd_GIVEN_fixtureRepoWithRegisteredWorktree_WHEN_run_THEN_noError(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	hostsFile := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(hostsFile, nil, 0o644); err != nil {
		t.Fatalf("write hostsFile: %v", err)
	}
	origHostsPath := hostsPath
	hostsPath = hostsFile
	t.Cleanup(func() { hostsPath = origHostsPath })

	configYAML := "version: 1\n" +
		"worktrees_dir: ../repo-worktrees\n" +
		"dns_pattern: \"local-{name}.dev.test\"\n" +
		"max_slots: 9\n" +
		"ports: {}\n" +
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

	if err := hostsCmd.RunE(hostsCmd, nil); err != nil {
		t.Fatalf("hosts: %v", err)
	}
	if err := hostsCmd.RunE(hostsCmd, []string{"feature"}); err != nil {
		t.Fatalf("hosts feature: %v", err)
	}
	if err := hostsCmd.RunE(hostsCmd, []string{"nonexistent"}); err == nil {
		t.Fatalf("expected error for unregistered worktree name")
	}
}
