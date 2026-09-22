// Package hooks runs a hook executable with the instance's env contract
// (see Env) and the new worktree as its working directory.
package hooks

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// Run executes the hook at hookPath, which must already be resolved to an
// absolute path.
func Run(hookPath string, wtPath string, env map[string]string) error {
	if hookPath == "" {
		return nil
	}

	if _, err := os.Stat(hookPath); err != nil {
		return fmt.Errorf("hooks: %w", err)
	}

	cmd := exec.Command(hookPath)
	cmd.Dir = wtPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), envLines(env)...)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("hooks: %s exited %d", filepath.Base(hookPath), exitErr.ExitCode())
		}
		return fmt.Errorf("hooks: run %s: %w", filepath.Base(hookPath), err)
	}
	return nil
}

func envLines(env map[string]string) []string {
	lines := make([]string, 0, len(env))
	for k, v := range env {
		lines = append(lines, k+"="+v)
	}
	return lines
}

func Env(name string, slot int, dns, wtPath string, ports map[string]int) map[string]string {
	env := make(map[string]string, len(ports)+4)
	env["CT_NAME"] = name
	env["CT_SLOT"] = strconv.Itoa(slot)
	env["CT_DNS"] = dns
	env["CT_WT_PATH"] = wtPath
	for portVar, value := range ports {
		env[portVar] = strconv.Itoa(value)
	}
	return env
}
