package slots_test

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/slots"
)

func TestLoad_GIVEN_noRegistryFile_WHEN_loaded_THEN_emptyRegistry(t *testing.T) {
	dir := t.TempDir()

	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.Slots()) != 0 {
		t.Fatalf("got %#v, want empty", reg.Slots())
	}
}

func TestAllocate_GIVEN_emptyRegistry_WHEN_allocated_THEN_lowestFreeSlotAssigned(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	slot, err := reg.Allocate("feature", 9)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if slot != 1 {
		t.Fatalf("got slot %d, want 1", slot)
	}
}

func TestAllocate_GIVEN_slotOneTaken_WHEN_allocated_THEN_nextFreeSlotAssigned(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := reg.Allocate("first", 9); err != nil {
		t.Fatalf("Allocate first: %v", err)
	}

	slot, err := reg.Allocate("second", 9)
	if err != nil {
		t.Fatalf("Allocate second: %v", err)
	}
	if slot != 2 {
		t.Fatalf("got slot %d, want 2", slot)
	}
}

func TestAllocate_GIVEN_gapFromFreedSlot_WHEN_allocated_THEN_reusesLowestFreeSlot(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := reg.Allocate("a", 9); err != nil {
		t.Fatalf("Allocate a: %v", err)
	}
	if _, err := reg.Allocate("b", 9); err != nil {
		t.Fatalf("Allocate b: %v", err)
	}
	if err := reg.Free("a"); err != nil {
		t.Fatalf("Free a: %v", err)
	}

	slot, err := reg.Allocate("c", 9)
	if err != nil {
		t.Fatalf("Allocate c: %v", err)
	}
	if slot != 1 {
		t.Fatalf("got slot %d, want 1 (reused freed slot)", slot)
	}
}

func TestAllocate_GIVEN_allSlotsTaken_WHEN_allocated_THEN_errors(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := reg.Allocate(string(rune('a'+i)), 3); err != nil {
			t.Fatalf("Allocate %d: %v", i, err)
		}
	}

	if _, err := reg.Allocate("overflow", 3); err == nil {
		t.Fatalf("expected error when max_slots is full")
	}
}

func TestAllocate_GIVEN_nameAlreadyRegistered_WHEN_allocatedAgain_THEN_errors(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := reg.Allocate("feature", 9); err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	if _, err := reg.Allocate("feature", 9); err == nil {
		t.Fatalf("expected error re-allocating an already-registered name")
	}
}

func TestFree_GIVEN_registeredName_WHEN_freed_THEN_slotNoLongerReturned(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := reg.Allocate("feature", 9); err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	if err := reg.Free("feature"); err != nil {
		t.Fatalf("Free: %v", err)
	}
	if _, ok := reg.Slot("feature"); ok {
		t.Fatalf("expected feature to be gone after Free")
	}
}

func TestFree_GIVEN_unregisteredName_WHEN_freed_THEN_noError(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := reg.Free("never-registered"); err != nil {
		t.Fatalf("Free: %v", err)
	}
}

func TestName_GIVEN_registeredSlot_WHEN_resolved_THEN_returnsName(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	slot, err := reg.Allocate("feature", 9)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	name, ok := reg.Name(slot)
	if !ok || name != "feature" {
		t.Fatalf("got name=%q ok=%v, want feature/true", name, ok)
	}
}

func TestName_GIVEN_unregisteredSlot_WHEN_resolved_THEN_notFound(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, ok := reg.Name(5); ok {
		t.Fatalf("expected slot 5 not found")
	}
}

func TestAllocateThenLoad_GIVEN_persistedRegistry_WHEN_reloaded_THEN_entriesSurvive(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := reg.Allocate("feature", 9); err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	reloaded, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("reload Load: %v", err)
	}
	slot, ok := reloaded.Slot("feature")
	if !ok || slot != 1 {
		t.Fatalf("got slot=%d ok=%v, want 1/true", slot, ok)
	}
}

func TestNextFree_GIVEN_slotOneTaken_WHEN_computed_THEN_returnsLowestFreeWithoutPersisting(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := reg.Allocate("first", 9); err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	slot, err := reg.NextFree(9)
	if err != nil {
		t.Fatalf("NextFree: %v", err)
	}
	if slot != 2 {
		t.Fatalf("got slot %d, want 2", slot)
	}

	// Calling it again must return the same answer — nothing was persisted.
	again, err := reg.NextFree(9)
	if err != nil {
		t.Fatalf("NextFree (again): %v", err)
	}
	if again != 2 {
		t.Fatalf("got slot %d on second call, want 2 (NextFree must not persist)", again)
	}
}

func TestNextFree_GIVEN_allSlotsTaken_WHEN_computed_THEN_errors(t *testing.T) {
	dir := t.TempDir()
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := reg.Allocate("a", 1); err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	if _, err := reg.NextFree(1); err == nil {
		t.Fatalf("expected error when max_slots is full")
	}
}

func TestBusyPorts_GIVEN_aPortWithAnActiveListener_WHEN_probed_THEN_reportedBusy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	busyPort := ln.Addr().(*net.TCPAddr).Port

	freeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	freePort := freeLn.Addr().(*net.TCPAddr).Port
	freeLn.Close() // released, should be free again for the probe

	busy := slots.BusyPorts(map[string]int{
		"BUSY_PORT": busyPort,
		"FREE_PORT": freePort,
	})

	if len(busy) != 1 || busy[0] != "BUSY_PORT ("+strconv.Itoa(busyPort)+")" {
		t.Fatalf("got %#v, want exactly BUSY_PORT flagged", busy)
	}
}

func TestAllocate_GIVEN_worktreesDirDoesNotExistYet_WHEN_allocated_THEN_dirCreatedOnDemand(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-yet-created")
	reg, err := slots.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := reg.Allocate("feature", 9); err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".slots")); statErr != nil {
		t.Fatalf("expected .slots file to be created: %v", statErr)
	}
}
