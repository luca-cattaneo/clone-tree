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

func (c *Config) InstanceVars(name string, slot int) map[string]string {
	vars := BaseVars(c.RepoRoot)
	vars["name"] = name
	vars["name_lower"] = strings.ToLower(name)
	vars["slot"] = strconv.Itoa(slot)
	for portVar, value := range c.PortValues(slot) {
		vars[portVar] = strconv.Itoa(value)
	}
	vars["dns"] = Expand(c.DNSPattern, vars)
	return vars
}

func (c *Config) PortValues(slot int) map[string]int {
	values := make(map[string]int, len(c.Ports))
	for name, p := range c.Ports {
		values[name] = *p.Base + slot*p.Step
	}
	return values
}

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
