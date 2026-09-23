// Package slots manages the name->slot registry backed by a flat
// "<root>/.git/clone-tree/slots" file (one "name:slot" line per entry)
package slots

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Registry struct {
	path    string
	entries map[string]int
}

// A missing file is not an error — it yields an empty registry.
func Load(root string) (*Registry, error) {
	path := filepath.Join(root, ".git", "clone-tree", "slots")

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

func (r *Registry) Free(name string) error {
	delete(r.entries, name)
	return r.save()
}

func (r *Registry) Slot(name string) (int, bool) {
	slot, ok := r.entries[name]
	return slot, ok
}

func (r *Registry) Name(slot int) (string, bool) {
	for name, s := range r.entries {
		if s == slot {
			return name, true
		}
	}
	return "", false
}

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
		return fmt.Errorf("create slots registry dir: %w", err)
	}
	if err := os.WriteFile(r.path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write slots registry: %w", err)
	}
	return nil
}
