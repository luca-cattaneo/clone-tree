package cli

import (
	"encoding/json"
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
	Use:               "start <name|slot>",
	Short:             "Start a worktree's compose stack",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(_ *cobra.Command, args []string) error {
		return runCompose(args[0], "up", "-d")
	},
}

var stopCmd = &cobra.Command{
	Use:               "stop <name|slot>",
	Short:             "Stop a worktree's compose stack",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeWorktreeNames,
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
	count, ok := composeRunningCount(dir, project)
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%d", count)
}

// composeRunningCount is runningContainers' underlying probe, surfacing the
// "docker/compose couldn't answer at all" case as ok=false instead of
// collapsing it into the same "-" as "confirmed zero containers running".
// doctor's busy-ports check needs that distinction: a worktree confirmed
// NOT running with a busy port is a real conflict (✗), but "couldn't tell"
// must not be reported as one (falls back to a ⚠ instead).
func composeRunningCount(dir, project string) (count int, ok bool) {
	out, err := composeRunner(dir, project, "ps", "-q", "--status", "running")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			count++
		}
	}
	return count, true
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

// composeDown runs `docker compose down -v --remove-orphans` for project in
// dir (typically a worktree about to be removed). --remove-orphans also
// tears down one-off containers (e.g. `docker compose run` husks) that
// belong to project but aren't declared in the compose file, so they never
// survive as residue after the worktree itself is gone. When dir has no
// compose file, nothing was ever brought up (docker may not even be
// installed) so a failure is swallowed; when a compose file is present, a
// real stack is expected to tear down cleanly and a failure is propagated.
func composeDown(dir, project string) error {
	_, err := composeRunner(dir, project, "down", "-v", "--remove-orphans")
	if err != nil && !hasComposeFile(dir) {
		return nil
	}
	return err
}

// composeProject is one entry of `docker compose ls -a --format json`'s
// output — only the field doctor's orphan-stacks check needs.
type composeProject struct {
	Name string `json:"Name"`
}

// composeProjects lists every docker compose project name known to the
// daemon (running or stopped, -a), via the composeRunner seam so it's
// stubbable without a real docker binary. project/dir don't matter to `ls`
// itself, but composeRunner needs values to build the command.
func composeProjects(root string) ([]string, error) {
	out, err := composeRunner(root, "", "ls", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}

	var projects []composeProject
	if err := json.Unmarshal(out, &projects); err != nil {
		return nil, err
	}

	names := make([]string, len(projects))
	for i, p := range projects {
		names[i] = p.Name
	}
	return names, nil
}

// composeProjectName is the COMPOSE_PROJECT_NAME ct assigns a worktree's
// stack: <repo>-<name>, lowercased — Docker Compose requires project names
// to be lowercase, and a worktree name is otherwise free-form (e.g.
// "documentCache"). Must match how a consumer config's env: block computes
// COMPOSE_PROJECT_NAME (via {name_lower}, see config.InstanceVars) so the
// project docker compose actually uses (this override) and the one recorded
// in the generated .env agree — a mismatch would leave `ct start`/`stop`
// operating on a different compose project than `docker compose` run
// directly in the worktree with its .env. Main is excluded from this
// scheme — it keeps docker compose's own default project name (its
// directory basename, i.e. repo itself), since main is never created/named
// by ct.
func composeProjectName(repo, name string) string {
	return strings.ToLower(repo + "-" + name)
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

	cfg, err := config.LoadExisting(root, configPath)
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
// <root>/.clone-tree/config.yaml) — start/stop must refuse cleanly in bare
// mode rather than auto-scaffold a config file as a side effect of a
// start/stop call.
func configFileExists(root string) bool {
	_, err := os.Stat(config.ResolvePath(root, configPath))
	return err == nil
}
