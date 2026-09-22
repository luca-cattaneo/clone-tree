package gitwt

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Worktree struct {
	Path     string
	Branch   string
	Bare     bool
	Detached bool
}

// ParsePorcelain parses the output of `git worktree list --porcelain` into
// a slice of Worktree.
func ParsePorcelain(output string) []Worktree {
	var result []Worktree
	var cur *Worktree

	flush := func() {
		if cur != nil {
			result = append(result, *cur)
			cur = nil
		}
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch "):
			if cur != nil {
				cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
			}
		case line == "bare":
			if cur != nil {
				cur.Bare = true
			}
		case line == "detached":
			if cur != nil {
				cur.Detached = true
			}
		}
	}
	flush()

	return result
}

func List(dir string) ([]Worktree, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree list: %s", strings.TrimSpace(stderr.String()))
	}
	return ParsePorcelain(stdout.String()), nil
}

func FindByName(worktrees []Worktree, name string) (Worktree, bool) {
	for _, wt := range worktrees {
		if filepath.Base(wt.Path) == name {
			return wt, true
		}
	}
	return Worktree{}, false
}

func refExists(dir, ref string) (bool, error) {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}

func BranchExists(dir, branch string) (bool, error) {
	return refExists(dir, "refs/heads/"+branch)
}

func RemoteBranchExists(dir, branch string) (bool, error) {
	return refExists(dir, "refs/remotes/origin/"+branch)
}

func baseStartPoint(dir, base string) (string, error) {
	remoteExists, err := RemoteBranchExists(dir, base)
	if err != nil {
		return "", err
	}
	if remoteExists {
		return "origin/" + base, nil
	}
	return base, nil
}

func Create(dir, path, branch, base string) error {
	exists, err := BranchExists(dir, branch)
	if err != nil {
		return err
	}

	var cmd *exec.Cmd
	switch {
	case exists:
		cmd = exec.Command("git", "worktree", "add", path, branch)
	default:
		fetchArgs := []string{"fetch", "origin", branch}
		if base != branch {
			fetchArgs = append(fetchArgs, base)
		}
		fetchCmd := exec.Command("git", fetchArgs...)
		fetchCmd.Dir = dir
		// Best-effort: ignore fetch errors, e.g. no remote configured or offline.
		_ = fetchCmd.Run()

		remoteExists, err := RemoteBranchExists(dir, branch)
		if err != nil {
			return err
		}
		if remoteExists {
			cmd = exec.Command("git", "worktree", "add", "--track", "-b", branch, path, "origin/"+branch)
		} else {
			start, err := baseStartPoint(dir, base)
			if err != nil {
				return err
			}
			cmd = exec.Command("git", "worktree", "add", "-b", branch, path, start)
		}
	}
	cmd.Dir = dir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree add: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func Remove(dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Prune removes administrative metadata for worktrees whose working
// directory no longer exists, e.g. one deleted by hand instead of via
// Remove.
func Prune(dir string) error {
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree prune: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}
