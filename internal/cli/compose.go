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

func composeCmd(dir, project string, args ...string) *exec.Cmd {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+project)
	return cmd
}

var composeRunner = func(dir, project string, args ...string) ([]byte, error) {
	return composeCmd(dir, project, args...).Output()
}

func runningContainers(dir, project string) string {
	count, ok := composeRunningCount(dir, project)
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%d", count)
}

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
// looks for, in precedence order.
var composeFileCandidates = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

func hasComposeFile(dir string) bool {
	for _, name := range composeFileCandidates {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// When dir has no compose file, a failure is swallowed; otherwise it is
// propagated.
func composeDown(dir, project string) error {
	_, err := composeRunner(dir, project, "down", "-v", "--remove-orphans")
	if err != nil && !hasComposeFile(dir) {
		return nil
	}
	return err
}

// composeProject is one entry of `docker compose ls -a --format json`'s
// output.
type composeProject struct {
	Name string `json:"Name"`
}

// composeProjects lists every docker compose project name known to the
// daemon (running or stopped, -a). project/dir don't matter to `ls`
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
// stack: <repo>-<name>, lowercased, since Docker Compose requires project
// names to be lowercase. Must match how config.InstanceVars computes
// COMPOSE_PROJECT_NAME (via {name_lower}) for the generated .env, or
// `ct start`/`stop` would operate on a different compose project than a
// direct `docker compose` invocation in the worktree.
func composeProjectName(repo, name string) string {
	return strings.ToLower(repo + "-" + name)
}

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

func configFileExists(root string) bool {
	_, err := os.Stat(config.ResolvePath(root, configPath))
	return err == nil
}
