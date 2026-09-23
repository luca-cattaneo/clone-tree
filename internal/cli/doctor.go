package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luca-cattaneo/clone-tree/internal/config"
	"github.com/luca-cattaneo/clone-tree/internal/gitwt"
	"github.com/luca-cattaneo/clone-tree/internal/hosts"
	"github.com/luca-cattaneo/clone-tree/internal/slots"
	"github.com/spf13/cobra"
)

var ErrChecksFailed = errors.New("doctor: one or more checks failed")

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the repo's clone-tree setup",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		dir, err := cwd()
		if err != nil {
			return err
		}

		root, err := gitwt.RepoRoot(dir)
		if err != nil {
			return err
		}

		groups := runDoctor(root, configPath)
		printDoctorGroups(os.Stdout, groups)
		if anyFailed(groups) {
			return ErrChecksFailed
		}
		return nil
	},
}

type severity int

const (
	sevOK severity = iota
	sevWarn
	sevFail
)

func (s severity) mark() string {
	switch s {
	case sevFail:
		return "✗"
	case sevWarn:
		return "⚠"
	default:
		return "✓"
	}
}

type doctorLine struct {
	sev severity
	msg string
}

type doctorGroup struct {
	title string
	lines []doctorLine
}

// checkLiteralPorts runs unconditionally since it reads compose files
// directly rather than .clone-tree/config.yaml. Every other check needs a
// loaded, valid config, so they are skipped — not reported as failing —
// when config is missing or invalid.
func runDoctor(root, overridePath string) []doctorGroup {
	cfg, configGroup := checkConfig(root, overridePath)
	groups := []doctorGroup{configGroup, checkLiteralPorts(root)}

	if cfg == nil {
		return groups
	}

	reg, err := slots.Load(root)
	if err != nil {
		return append(groups, doctorGroup{
			title: "busy ports",
			lines: []doctorLine{{sevFail, fmt.Sprintf("load slots registry: %v", err)}},
		})
	}

	return append(groups,
		checkBusyPorts(root, cfg, reg),
		checkOrphanSlots(root, reg),
		checkOrphanStacks(root, reg),
		checkHooks(root, cfg),
		checkDNS(cfg, reg),
	)
}

func checkConfig(root, overridePath string) (*config.Config, doctorGroup) {
	cfg, err := config.LoadExisting(root, overridePath)
	switch {
	case err == nil:
		return cfg, doctorGroup{title: "config", lines: []doctorLine{{sevOK, "config.yaml valid"}}}
	case errors.Is(err, config.ErrNoConfig):
		return nil, doctorGroup{title: "config", lines: []doctorLine{{sevWarn, "no config — bare mode; run ct create-config"}}}
	default:
		return nil, doctorGroup{title: "config", lines: []doctorLine{{sevFail, err.Error()}}}
	}
}

func checkLiteralPorts(root string) doctorGroup {
	bindings, err := config.ScanLiteralPortBindings(root)
	if err != nil {
		return doctorGroup{title: "ports", lines: []doctorLine{{sevFail, fmt.Sprintf("scan compose ports: %v", err)}}}
	}
	if len(bindings) == 0 {
		return doctorGroup{title: "ports", lines: []doctorLine{{sevOK, "no literal (non-${VAR}) host port bindings"}}}
	}

	lines := make([]doctorLine, len(bindings))
	for i, b := range bindings {
		lines[i] = doctorLine{sevWarn, fmt.Sprintf("service %s: literal host port %d cannot be replicated", b.Service, b.Port)}
	}
	return doctorGroup{title: "ports", lines: lines}
}

// checkBusyPorts also checks slot 0 (main), but only ever as a ⚠: main may
// legitimately run its own compose stack on those ports, and has no
// per-worktree stack to check a busy finding against.
func checkBusyPorts(root string, cfg *config.Config, reg *slots.Registry) doctorGroup {
	if len(cfg.Ports) == 0 {
		return doctorGroup{title: "busy ports", lines: []doctorLine{{sevOK, "no ports configured"}}}
	}

	var lines []doctorLine
	for _, name := range sortedRegistryNames(reg) {
		slot, _ := reg.Slot(name)
		if line, ok := busyPortsLineForWorktree(root, name, slot, cfg); ok {
			lines = append(lines, line)
		}
	}
	if busy := slots.BusyPorts(cfg.PortValues(0)); len(busy) > 0 {
		lines = append(lines, doctorLine{sevWarn, fmt.Sprintf("main (slot 0): busy ports: %s", strings.Join(busy, ", "))})
	}

	if len(lines) == 0 {
		lines = []doctorLine{{sevOK, "no busy ports"}}
	}
	return doctorGroup{title: "busy ports", lines: lines}
}

// busyPortsLineForWorktree resolves one registered worktree's busy-ports
// line; ok=false means nothing worth reporting. A running stack (count >
// 0) short-circuits to an informational ✓ without probing ports at all —
// a busy port explained by the worktree's own stack isn't a finding. A
// confirmed-not-running stack (count == 0) still probes ports, and a hit
// is a genuine external conflict (✗). When docker/compose can't answer at
// all (!ok), a busy finding can't be told apart from the worktree's own
// stack, so it's downgraded to a ⚠ instead of a ✗.
func busyPortsLineForWorktree(root, name string, slot int, cfg *config.Config) (doctorLine, bool) {
	dir := gitwt.WorktreePath(root, name)
	project := composeProjectName(filepath.Base(root), name)

	count, ok := composeRunningCount(dir, project)
	if ok && count > 0 {
		return doctorLine{sevOK, fmt.Sprintf("%s (slot %d): stack running (%d containers)", name, slot, count)}, true
	}

	busy := slots.BusyPorts(cfg.PortValues(slot))
	if len(busy) == 0 {
		return doctorLine{}, false
	}

	sev := sevFail
	if !ok {
		sev = sevWarn
	}
	return doctorLine{sev, fmt.Sprintf("%s (slot %d): busy ports: %s", name, slot, strings.Join(busy, ", "))}, true
}

func checkOrphanSlots(root string, reg *slots.Registry) doctorGroup {
	registered := reg.Slots()

	var lines []doctorLine
	for _, name := range sortedRegistryNames(reg) {
		if _, err := os.Stat(gitwt.WorktreePath(root, name)); errors.Is(err, os.ErrNotExist) {
			lines = append(lines, doctorLine{sevFail, fmt.Sprintf("orphan slot %d (%s) — run ct remove %s", registered[name], name, name)})
		}
	}

	worktrees, err := gitwt.List(root)
	if err != nil {
		lines = append(lines, doctorLine{sevFail, fmt.Sprintf("list worktrees: %v", err)})
	}
	prefix := filepath.Base(root) + "-"
	for _, wt := range worktrees {
		if !strings.HasPrefix(filepath.Base(wt.Path), prefix) {
			continue
		}
		if _, ok := registered[gitwt.WorktreeName(root, wt.Path)]; !ok {
			lines = append(lines, doctorLine{sevWarn, fmt.Sprintf("unregistered worktree %s", wt.Path)})
		}
	}

	if len(lines) == 0 {
		lines = []doctorLine{{sevOK, "no orphan slots"}}
	}
	return doctorGroup{title: "orphan slots", lines: lines}
}

// checkOrphanStacks flags docker compose projects matching this repo's
// prefix that no longer correspond to any registered worktree. The bare
// repo project (main's own, unprefixed) is never flagged.
func checkOrphanStacks(root string, reg *slots.Registry) doctorGroup {
	repo := filepath.Base(root)

	projects, err := composeProjects(root)
	if err != nil {
		return doctorGroup{title: "orphan stacks", lines: []doctorLine{{sevWarn, fmt.Sprintf("docker compose ls unavailable — skipped (%v)", err)}}}
	}

	known := map[string]bool{strings.ToLower(repo): true}
	for name := range reg.Slots() {
		known[composeProjectName(repo, name)] = true
	}
	prefix := strings.ToLower(repo) + "-"

	var lines []doctorLine
	for _, p := range projects {
		if known[p] || !strings.HasPrefix(p, prefix) {
			continue
		}
		lines = append(lines, doctorLine{sevFail, fmt.Sprintf("orphan compose project %s — docker compose -p %s down -v --remove-orphans", p, p)})
	}

	if len(lines) == 0 {
		lines = []doctorLine{{sevOK, "no orphan compose projects"}}
	}
	return doctorGroup{title: "orphan stacks", lines: lines}
}

func checkHooks(root string, cfg *config.Config) doctorGroup {
	var lines []doctorLine
	if line, ok := checkHookPath("post_create", hookAbsPath(root, cfg.Hooks.PostCreate)); !ok {
		lines = append(lines, line)
	}
	if line, ok := checkHookPath("pre_remove", hookAbsPath(root, cfg.Hooks.PreRemove)); !ok {
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		lines = []doctorLine{{sevOK, "no hooks configured, or all present and executable"}}
	}
	return doctorGroup{title: "hooks", lines: lines}
}

// checkHookPath: "" means unconfigured. ok is true when there's nothing to
// report.
func checkHookPath(label, absPath string) (doctorLine, bool) {
	if absPath == "" {
		return doctorLine{}, true
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return doctorLine{sevFail, fmt.Sprintf("%s hook not found: %s", label, absPath)}, false
	}
	if info.Mode()&0o111 == 0 {
		return doctorLine{sevFail, fmt.Sprintf("%s hook not executable: %s", label, absPath)}, false
	}
	return doctorLine{}, true
}

// checkDNS flags a missing /etc/hosts entry as a ⚠, not a ✗: an unmanaged
// line already bound to the same dns counts as good enough.
func checkDNS(cfg *config.Config, reg *slots.Registry) doctorGroup {
	if cfg.DNSPattern == "" {
		return doctorGroup{title: "dns", lines: []doctorLine{{sevOK, "dns_pattern not set — skipped"}}}
	}

	var lines []doctorLine
	for _, name := range sortedRegistryNames(reg) {
		slot, _ := reg.Slot(name)
		dns := cfg.InstanceVars(name, slot)["dns"]

		owned, err := hosts.Has(hostsPath, name)
		if err != nil {
			lines = append(lines, doctorLine{sevFail, fmt.Sprintf("%s: %v", name, err)})
			continue
		}
		if owned {
			continue
		}

		unmanaged, err := hosts.HasDNS(hostsPath, dns)
		if err != nil {
			lines = append(lines, doctorLine{sevFail, fmt.Sprintf("%s: %v", name, err)})
			continue
		}
		if !unmanaged {
			lines = append(lines, doctorLine{sevWarn, fmt.Sprintf("%s: missing /etc/hosts entry for %s", name, dns)})
		}
	}

	if len(lines) == 0 {
		lines = []doctorLine{{sevOK, "all registered worktrees present in the hosts file"}}
	}
	return doctorGroup{title: "dns", lines: lines}
}

func sortedRegistryNames(reg *slots.Registry) []string {
	slotByName := reg.Slots()
	names := make([]string, 0, len(slotByName))
	for name := range slotByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func anyFailed(groups []doctorGroup) bool {
	for _, g := range groups {
		for _, l := range g.lines {
			if l.sev == sevFail {
				return true
			}
		}
	}
	return false
}

func printDoctorGroups(w io.Writer, groups []doctorGroup) {
	for i, g := range groups {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintf(w, "%s:\n", g.title)
		for _, l := range g.lines {
			_, _ = fmt.Fprintf(w, "  %s %s\n", l.sev.mark(), l.msg)
		}
	}
}
