package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/config"
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
