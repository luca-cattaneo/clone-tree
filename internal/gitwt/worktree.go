package gitwt

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree describes one entry from `git worktree list --porcelain`.
type Worktree struct {
	Path     string
	Branch   string
	Bare     bool
	Detached bool
}

// ParsePorcelain parses the output of `git worktree list --porcelain` into
// a slice of Worktree. It is a pure function so it is exhaustively
// unit-testable without a real git repo.
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

// List returns all worktrees registered against the main repo containing dir.
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

// FindByName returns the worktree whose path basename matches name.
func FindByName(worktrees []Worktree, name string) (Worktree, bool) {
	for _, wt := range worktrees {
		if filepath.Base(wt.Path) == name {
			return wt, true
		}
	}
	return Worktree{}, false
}

// refExists reports whether ref exists in the repo at dir.
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

// BranchExists reports whether refs/heads/<branch> exists in the repo at dir.
func BranchExists(dir, branch string) (bool, error) {
	return refExists(dir, "refs/heads/"+branch)
}

// RemoteBranchExists reports whether refs/remotes/origin/<branch> exists in
// the repo at dir.
func RemoteBranchExists(dir, branch string) (bool, error) {
	return refExists(dir, "refs/remotes/origin/"+branch)
}

// baseStartPoint returns "origin/<base>" if that remote ref exists, else
// base itself, for use as the start-point of a new branch.
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

// Create adds a new worktree at path for branch. If branch does not exist
// locally, it fetches origin/<branch> (and origin/<base> in the same
// best-effort call, ignoring fetch errors, e.g. no remote configured or
// offline) and, when the remote branch exists, creates branch tracking it;
// otherwise it creates branch from base — starting from origin/<base> when
// that remote ref exists, else from the local base ref.
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

// Remove removes the worktree at path.
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
// directory no longer exists — e.g. one deleted by hand (rm -rf) instead of
// via Remove, which Remove itself already tolerates but doesn't always
// clean up after. Best-effort by design in every caller: a prune failure
// must never block `ct remove`/`ct create`'s rollback from completing.
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
