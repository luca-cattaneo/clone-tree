// Package slots manages the name->slot registry backed by a flat
// "<worktrees-dir>/.slots" file (one "name:slot" line per entry)
package slots

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Registry is the in-memory, file-backed name->slot mapping for one
// worktrees directory.
type Registry struct {
	path    string
	entries map[string]int
}

// Load reads "<worktreesDir>/.slots". A missing file is not an error — it
// means no worktree has been created yet — and yields an empty registry.
func Load(worktreesDir string) (*Registry, error) {
	path := filepath.Join(worktreesDir, ".slots")

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Registry{path: path, entries: map[string]int{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read slots registry: %w", err)
	}

	entries := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		name, slotStr, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		slot, err := strconv.Atoi(slotStr)
		if err != nil {
			continue
		}
		entries[name] = slot
	}
	return &Registry{path: path, entries: entries}, nil
}

// Allocate assigns the lowest free slot in 1..maxSlots to name and persists
// the registry.
func (r *Registry) Allocate(name string, maxSlots int) (int, error) {
	if slot, ok := r.entries[name]; ok {
		return 0, fmt.Errorf("slots: %q is already registered at slot %d", name, slot)
	}

	slot, err := r.NextFree(maxSlots)
	if err != nil {
		return 0, err
	}

	r.entries[name] = slot
	if err := r.save(); err != nil {
		delete(r.entries, name)
		return 0, err
	}
	return slot, nil
}

// NextFree returns the lowest free slot in 1..maxSlots without persisting
// anything, so a caller (e.g. the busy-port probe in create) can inspect
// the candidate slot's ports before committing to an allocation.
func (r *Registry) NextFree(maxSlots int) (int, error) {
	used := make(map[int]bool, len(r.entries))
	for _, s := range r.entries {
		used[s] = true
	}
	for slot := 1; slot <= maxSlots; slot++ {
		if !used[slot] {
			return slot, nil
		}
	}
	return 0, fmt.Errorf("slots: no free slot (max_slots=%d)", maxSlots)
}

// Free removes name from the registry and persists it. Freeing a name that
// isn't registered is a no-op.
func (r *Registry) Free(name string) error {
	delete(r.entries, name)
	return r.save()
}

// Slot returns the slot registered for name, if any.
func (r *Registry) Slot(name string) (int, bool) {
	slot, ok := r.entries[name]
	return slot, ok
}

// Name resolves a slot number back to its registered name.
func (r *Registry) Name(slot int) (string, bool) {
	for name, s := range r.entries {
		if s == slot {
			return name, true
		}
	}
	return "", false
}

// Slots returns a copy of the full name->slot mapping.
func (r *Registry) Slots() map[string]int {
	out := make(map[string]int, len(r.entries))
	for k, v := range r.entries {
		out[k] = v
	}
	return out
}

func (r *Registry) save() error {
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s:%d\n", name, r.entries[name])
	}

	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("create worktrees dir: %w", err)
	}
	if err := os.WriteFile(r.path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write slots registry: %w", err)
	}
	return nil
}
