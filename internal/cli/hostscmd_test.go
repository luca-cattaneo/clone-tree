package cli

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestHostsRow_GIVEN_unmanagedLineForDNS_WHEN_built_THEN_checkmarkUnmanagedColumn(t *testing.T) {
	hostsFile := filepath.Join(t.TempDir(), "hosts")
	// No "# clone-tree:" marker, i.e. an unmanaged entry.
	if err := os.WriteFile(hostsFile, []byte("127.0.0.1 local-feature.dev.test\n"), 0o644); err != nil {
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
	want := []string{"1", "feature", "local-feature.dev.test", "✓ (unmanaged)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestUrlRows_GIVEN_configuredUrls_WHEN_built_THEN_sortedByLabelWithTemplatesExpanded(t *testing.T) {
	cfg := &config.Config{
		RepoRoot:   "/home/dev/myrepo",
		DNSPattern: "local-{name}.dev.test",
		Ports: map[string]config.Port{
			"PROXY_HTTPS_PORT": {Base: intPtr(10443), Step: 100},
		},
		Urls: map[string]string{
			"Web Client":  "https://{dns}:{PROXY_HTTPS_PORT}/",
			"Buggregator": "http://localhost:8010/",
		},
	}

	got := urlRows(cfg, "feature", 1)

	want := [][]string{
		{"Buggregator", "http://localhost:8010/"},
		{"Web Client", "https://local-feature.dev.test:10543/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestHostsCmd_GIVEN_configuredUrls_WHEN_runWithName_THEN_serviceUrlTableRendered(t *testing.T) {
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
		"base_branch: main\n" +
		"dns_pattern: \"local-{name}.dev.test\"\n" +
		"max_slots: 9\n" +
		"ports:\n  PROXY_HTTPS_PORT: {base: 10443, step: 100}\n" +
		"env: {}\n" +
		"files: {}\n" +
		"hooks: {}\n" +
		"urls:\n  Web Client: \"https://{dns}:{PROXY_HTTPS_PORT}/\"\n"
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

	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("Pipe: %v", pipeErr)
	}
	origStdout := os.Stdout
	os.Stdout = w
	err := hostsCmd.RunE(hostsCmd, []string{"feature"})
	_ = w.Close()
	os.Stdout = origStdout
	if err != nil {
		t.Fatalf("hosts feature: %v", err)
	}

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("ReadAll: %v", readErr)
	}
	got := string(out)
	if !strings.Contains(got, "Service") || !strings.Contains(got, "URL") {
		t.Fatalf("expected a Service/URL table, got:\n%s", got)
	}
	if !strings.Contains(got, "Web Client") || !strings.Contains(got, "https://local-feature.dev.test:10543/") {
		t.Fatalf("expected the expanded Web Client URL, got:\n%s", got)
	}
}

func TestHostsCmd_GIVEN_noConfigInRepo_WHEN_run_THEN_errorsWithoutScaffolding(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	chdir(t, repoDir)
	configPath = ""

	if err := hostsCmd.RunE(hostsCmd, nil); err == nil {
		t.Fatalf("expected an error when no .clone-tree config exists")
	}

	if _, statErr := os.Stat(filepath.Join(repoDir, ".clone-tree")); !os.IsNotExist(statErr) {
		t.Fatalf("expected hosts to never scaffold .clone-tree/, stat err: %v", statErr)
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
		"base_branch: main\n" +
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
	if err := hostsCmd.RunE(hostsCmd, []string{"1"}); err != nil {
		t.Fatalf("hosts by slot number: %v", err)
	}
}
