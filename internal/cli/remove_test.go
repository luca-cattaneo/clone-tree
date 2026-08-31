package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/hosts"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
)

func newTestRegistry(t *testing.T, entries map[string]int) *slots.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("slots.Load: %v", err)
	}
	for name := range entries {
		if _, err := reg.Allocate(name, 9); err != nil {
			t.Fatalf("Allocate %q: %v", name, err)
		}
	}
	return reg
}

func TestResolveNameOrSlot_GIVEN_nameArg_WHEN_resolved_THEN_returnedAsIs(t *testing.T) {
	reg := newTestRegistry(t, nil)

	got, err := resolveNameOrSlot(reg, "feature")
	if err != nil {
		t.Fatalf("resolveNameOrSlot: %v", err)
	}
	if got != "feature" {
		t.Fatalf("got %q, want feature", got)
	}
}

func TestResolveNameOrSlot_GIVEN_registeredSlotNumber_WHEN_resolved_THEN_returnsRegisteredName(t *testing.T) {
	reg := newTestRegistry(t, map[string]int{"feature": 0})
	slot, _ := reg.Slot("feature")

	got, err := resolveNameOrSlot(reg, strconv.Itoa(slot))
	if err != nil {
		t.Fatalf("resolveNameOrSlot: %v", err)
	}
	if got != "feature" {
		t.Fatalf("got %q, want feature", got)
	}
}

func TestResolveNameOrSlot_GIVEN_slotZero_WHEN_resolved_THEN_errorsCannotRemoveMain(t *testing.T) {
	reg := newTestRegistry(t, nil)

	if _, err := resolveNameOrSlot(reg, "0"); err == nil {
		t.Fatalf("expected error for slot 0 (main repo)")
	}
}

func TestResolveNameOrSlot_GIVEN_unregisteredSlotNumber_WHEN_resolved_THEN_errors(t *testing.T) {
	reg := newTestRegistry(t, nil)

	if _, err := resolveNameOrSlot(reg, "5"); err == nil {
		t.Fatalf("expected error for unregistered slot")
	}
}

func TestRemoveCloneCoWDsts_GIVEN_existingDst_WHEN_called_THEN_dstDeleted(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "data_feature")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg := &config.Config{
		Files: config.Files{
			CloneCoW: []config.CloneCoW{
				{Src: filepath.Join(dir, "data"), Dst: filepath.Join(dir, "data_{name}")},
			},
		},
	}

	removeCloneCoWDsts(cfg, "feature", 1)

	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err: %v", dst, err)
	}
}

func TestRemoveCloneCoWDsts_GIVEN_missingDst_WHEN_called_THEN_noopWithoutError(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Files: config.Files{
			CloneCoW: []config.CloneCoW{
				{Src: filepath.Join(dir, "data"), Dst: filepath.Join(dir, "data_{name}")},
			},
		},
	}

	// Must not panic or write anything for an already-absent destination.
	removeCloneCoWDsts(cfg, "feature", 1)
}

func TestRemoveCmd_GIVEN_noConfigInRepo_WHEN_run_THEN_errorsWithoutScaffolding(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	chdir(t, repoDir)
	configPath = ""

	if err := removeCmd.RunE(removeCmd, []string{"feature"}); err == nil {
		t.Fatalf("expected an error when no .clone-tree config exists")
	}

	if _, statErr := os.Stat(filepath.Join(repoDir, ".clone-tree")); !os.IsNotExist(statErr) {
		t.Fatalf("expected remove to never scaffold .clone-tree/, stat err: %v", statErr)
	}
}

func TestRemove_GIVEN_preRemoveHookAndDNSPattern_WHEN_removed_THEN_hookRunsAndHostsEntryDropped(t *testing.T) {
	projectsDir, repoDir := newCreateFixtureRepo(t)

	markerFile := filepath.Join(projectsDir, "pre-remove-marker.txt")
	writeHookScript(t, repoDir, filepath.Join(".clone-tree", "hooks", "pre-remove.sh"),
		"env | sort > "+markerFile+"\n")

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
		"hooks:\n" +
		"  pre_remove: .clone-tree/hooks/pre-remove.sh\n"
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

	present, err := hosts.Has(hostsFile, "feature")
	if err != nil {
		t.Fatalf("hosts.Has after create: %v", err)
	}
	if !present {
		t.Fatalf("expected create to register a hosts entry for feature")
	}

	removeForce = true
	if err := removeCmd.RunE(removeCmd, []string{"feature"}); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("ReadFile marker: expected pre_remove hook to run: %v", err)
	}
	if !strings.Contains(string(got), "CT_NAME=feature") {
		t.Errorf("hook env %q does not contain CT_NAME=feature", got)
	}

	present, err = hosts.Has(hostsFile, "feature")
	if err != nil {
		t.Fatalf("hosts.Has after remove: %v", err)
	}
	if present {
		t.Fatalf("expected remove to drop the hosts entry for feature")
	}
}
