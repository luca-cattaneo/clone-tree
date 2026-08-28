package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// InstanceVars returns the templating vars available when materializing one
// worktree instance: {name}, {slot}, {dns}, {repo}, {projects_dir}, plus one
// entry per ports var holding its computed value for slot.
func (c *Config) InstanceVars(name string, slot int) map[string]string {
	vars := BaseVars(c.RepoRoot)
	vars["name"] = name
	vars["slot"] = strconv.Itoa(slot)
	for portVar, value := range c.PortValues(slot) {
		vars[portVar] = strconv.Itoa(value)
	}
	vars["dns"] = Expand(c.DNSPattern, vars)
	return vars
}

// PortValues computes the concrete port for every configured ports var at
// slot: base + slot*step.
func (c *Config) PortValues(slot int) map[string]int {
	values := make(map[string]int, len(c.Ports))
	for name, p := range c.Ports {
		values[name] = *p.Base + slot*p.Step
	}
	return values
}

// RenderEnv builds the .env file content for one worktree instance: one
// line per ports var, then templated env: entries, then CT_NAME/CT_SLOT.
// Keys within each group are sorted for reproducible output.
func (c *Config) RenderEnv(name string, slot int) string {
	vars := c.InstanceVars(name, slot)

	var b strings.Builder
	for _, k := range sortedKeys(c.Ports) {
		fmt.Fprintf(&b, "%s=%s\n", k, vars[k])
	}
	for _, k := range sortedKeys(c.Env) {
		fmt.Fprintf(&b, "%s=%s\n", k, Expand(c.Env[k], vars))
	}
	fmt.Fprintf(&b, "CT_NAME=%s\n", name)
	fmt.Fprintf(&b, "CT_SLOT=%d\n", slot)
	return b.String()
}

// WriteEnv writes <worktreeDir>/.env for name at slot. When
// <c.RepoRoot>/.env exists, it is inherited: copied verbatim minus any line
// whose KEY is one ct itself writes (a port var, an env: key, CT_NAME,
// CT_SLOT), then the ct block is appended under a labeled header so the
// boundary between inherited and generated content is visible. With no
// main .env, the file holds only the ct block (see RenderEnv) — nothing to
// label a boundary against.
func (c *Config) WriteEnv(worktreeDir, name string, slot int) error {
	content, err := c.renderFullEnv(name, slot)
	if err != nil {
		return err
	}
	path := filepath.Join(worktreeDir, ".env")
	return os.WriteFile(path, []byte(content), 0o644)
}

func (c *Config) renderFullEnv(name string, slot int) (string, error) {
	block := c.RenderEnv(name, slot)

	mainData, err := os.ReadFile(filepath.Join(c.RepoRoot, ".env"))
	if errors.Is(err, os.ErrNotExist) {
		return block, nil
	}
	if err != nil {
		return "", fmt.Errorf("read main .env: %w", err)
	}

	inherited := stripWrittenKeys(string(mainData), c.writtenKeys())
	header := fmt.Sprintf("# ── ct (slot %d) ─────────────\n", slot)
	return inherited + header + block, nil
}

// writtenKeys is every KEY ct itself writes into the .env block: every
// ports var, every env: key, plus CT_NAME/CT_SLOT. Any of these present in
// the inherited main .env would just be immediately shadowed by the
// appended block (dotenv readers keep the last occurrence), so they are
// stripped instead of left as confusing dead lines.
func (c *Config) writtenKeys() map[string]bool {
	keys := map[string]bool{"CT_NAME": true, "CT_SLOT": true}
	for k := range c.Ports {
		keys[k] = true
	}
	for k := range c.Env {
		keys[k] = true
	}
	return keys
}

// stripWrittenKeys drops every non-comment, non-blank line of content whose
// KEY (left of "=") is in keys, and ensures the result ends with exactly
// one trailing newline so the appended header starts on its own line.
func stripWrittenKeys(content string, keys map[string]bool) string {
	var kept []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if key, _, found := strings.Cut(trimmed, "="); found && keys[strings.TrimSpace(key)] {
				continue
			}
		}
		kept = append(kept, line)
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n\n"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
