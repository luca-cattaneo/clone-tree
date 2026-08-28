package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start <name|slot>",
	Short: "Start a worktree's compose stack",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return runCompose(args[0], "up", "-d")
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop <name|slot>",
	Short: "Stop a worktree's compose stack",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return runCompose(args[0], "down")
	},
}

// composeCmd builds a `docker compose <args...>` command that runs in dir
// with COMPOSE_PROJECT_NAME=project, ready for the caller to wire up
// stdio (start/stop inherit it) or capture output (list's Containers
// column, via composeRunner below).
func composeCmd(dir, project string, args ...string) *exec.Cmd {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+project)
	return cmd
}

// composeRunner executes a compose command and returns its captured
// output. It is a package-level seam so callers that only need output
// (runningContainers) can be tested without a real docker binary.
var composeRunner = func(dir, project string, args ...string) ([]byte, error) {
	return composeCmd(dir, project, args...).Output()
}

// runningContainers returns the count of running containers for project in
// dir, as a string. docker being unavailable, the compose call erroring, or
// there being no compose project at all all collapse to "-" — list must
// never fail just because docker isn't installed or a worktree has no
// stack yet.
func runningContainers(dir, project string) string {
	out, err := composeRunner(dir, project, "ps", "-q", "--status", "running")
	if err != nil {
		return "-"
	}
	var count int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			count++
		}
	}
	return fmt.Sprintf("%d", count)
}

// composeFileCandidates are the compose base filenames docker compose itself
// looks for, in precedence order — the same list internal/config's
// defaultComposeFiles checks for scaffolding. Duplicated here (unexported,
// four literals) rather than exporting a config API for this one caller.
var composeFileCandidates = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

// hasComposeFile reports whether dir has any compose base file.
func hasComposeFile(dir string) bool {
	for _, name := range composeFileCandidates {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// composeDown runs `docker compose down -v` for project in dir (typically a
// worktree about to be removed). When dir has no compose file, nothing was
// ever brought up (docker may not even be installed) so a failure is
// swallowed; when a compose file is present, a real stack is expected to
// tear down cleanly and a failure is propagated.
func composeDown(dir, project string) error {
	_, err := composeRunner(dir, project, "down", "-v")
	if err != nil && !hasComposeFile(dir) {
		return nil
	}
	return err
}

// composeProjectName is the COMPOSE_PROJECT_NAME ct assigns a worktree's
// stack: <repo>-<name>. Main is excluded from this scheme — it keeps
// docker compose's own default project name (its directory basename,
// i.e. repo itself), since main is never created/named by ct.
func composeProjectName(repo, name string) string {
	return repo + "-" + name
}

// runCompose resolves arg (a worktree name or registered slot, same
// resolution as `ct remove`) to its worktree directory and runs
// `docker compose <args...>` there with stdio inherited.
func runCompose(arg string, args ...string) error {
	dir, err := cwd()
	if err != nil {
		return err
	}

	root, err := gitwt.RepoRoot(dir)
	if err != nil {
		return err
	}

	if !configFileExists(root) {
		return fmt.Errorf("start/stop require a .clone-tree config")
	}

	cfg, err := config.Load(root, configPath)
	if err != nil {
		return err
	}

	reg, err := slots.Load(cfg.WorktreesDir)
	if err != nil {
		return err
	}

	name, err := resolveNameOrSlot(reg, arg)
	if err != nil {
		return err
	}

	wt, err := resolveWorktree(root, name)
	if err != nil {
		return err
	}

	project := composeProjectName(filepath.Base(root), name)
	cmd := composeCmd(wt.Path, project, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// configFileExists reports whether the config ct would load for root
// already exists on disk (the --config override path, or the default
// <root>/.clone-tree/config.yaml), without triggering config.Load's
// auto-scaffold side effect — start/stop must refuse cleanly in bare mode
// rather than generate a config file as a side effect of a start/stop call.
func configFileExists(root string) bool {
	path := configPath
	if path == "" {
		path = filepath.Join(root, ".clone-tree", "config.yaml")
	}
	_, err := os.Stat(path)
	return err == nil
}
