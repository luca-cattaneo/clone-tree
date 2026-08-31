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

// ErrChecksFailed is returned by doctorCmd's RunE when at least one check
// printed a ✗ line. The report itself (already written to stdout before
// RunE returns) explains what failed, so main.go recognizes this sentinel
// and exits 1 without also wrapping it in the usual "ct: <err>" line.
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

// severity is one doctorLine's ✓/✗/⚠ status.
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

// doctorLine is one status line under a doctorGroup.
type doctorLine struct {
	sev severity
	msg string
}

// doctorGroup is one titled section of `ct doctor`'s output (config, ports,
// busy ports, orphan slots, hooks, dns).
type doctorGroup struct {
	title string
	lines []doctorLine
}

// runDoctor runs every check in order and collects their groups. config and
// the literal-port-bindings check run unconditionally (root alone is
// enough — the latter reads compose files directly, not .clone-tree/
// config.yaml); every other check needs a loaded, valid config (a slot
// registry, ports:, hooks:, dns_pattern all live there), so they are
// skipped — not reported as failing — when config is missing (bare mode) or
// invalid.
func runDoctor(root, overridePath string) []doctorGroup {
	cfg, configGroup := checkConfig(root, overridePath)
	groups := []doctorGroup{configGroup, checkLiteralPorts(root)}

	if cfg == nil {
		return groups
	}

	reg, err := slots.Load(cfg.WorktreesDir)
	if err != nil {
		return append(groups, doctorGroup{
			title: "busy ports",
			lines: []doctorLine{{sevFail, fmt.Sprintf("load slots registry: %v", err)}},
		})
	}

	return append(groups,
		checkBusyPorts(root, cfg, reg),
		checkOrphanSlots(cfg, reg),
		checkHooks(root, cfg),
		checkDNS(cfg, reg),
	)
}

// checkConfig loads .clone-tree/config.yaml the same way every other
// command does. A missing config is a ⚠ (bare mode is a legitimate state —
// M1 promises `ct list` works on any git repo), not a ✗; a config that
// exists but fails to parse/validate is a ✗. Returns a nil *config.Config
// whenever the config isn't usable, so runDoctor knows to skip the checks
// that depend on it.
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

// checkLiteralPorts surfaces every host-port binding in root's compose
// files that isn't ${VAR}-parameterized — clone-tree can never make one
// slot-aware, since it never rewrites compose YAML.
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

// checkBusyPorts probes every registered worktree's computed ports
// (slots.BusyPorts, the same probe `ct create` runs before provisioning),
// but only once its own compose stack is confirmed NOT running — a running
// stack is expected to hold every one of its own ports, so that's success
// (an informational ✓), not a conflict; probing anyway would just rediscover
// the worktree's own containers and misreport them as a collision with
// something else. Plus slot 0's (main's) base values as a ⚠ only: main
// legitimately running its own compose stack on those ports isn't a
// problem, and main has no per-worktree stack of its own to check against.
func checkBusyPorts(root string, cfg *config.Config, reg *slots.Registry) doctorGroup {
	if len(cfg.Ports) == 0 {
		return doctorGroup{title: "busy ports", lines: []doctorLine{{sevOK, "no ports configured"}}}
	}

	repo := filepath.Base(root)
	var lines []doctorLine
	for _, name := range sortedRegistryNames(reg) {
		slot, _ := reg.Slot(name)
		if line, ok := busyPortsLineForWorktree(cfg.WorktreesDir, repo, name, slot, cfg); ok {
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
// line, ok=false meaning nothing worth reporting: its own compose stack's
// container count (composeRunningCount, same seam as list's Containers
// column) decides how a busy finding is judged — running (count > 0) short-
// circuits straight to an informational ✓ without probing ports at all (a
// busy port explained by the worktree's own stack isn't a finding); NOT
// running (confirmed count == 0) still probes ports, and a hit is a genuine
// external conflict (✗); when docker/compose can't answer at all (count,
// ok := ..., !ok — docker missing, compose erroring), a busy finding can't
// be told apart from "it's just this worktree's own stack" so it is
// downgraded to a ⚠ instead of a ✗ rather than risk a false failure.
func busyPortsLineForWorktree(worktreesDir, repo, name string, slot int, cfg *config.Config) (doctorLine, bool) {
	dir := filepath.Join(worktreesDir, name)
	project := composeProjectName(repo, name)

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

// checkOrphanSlots flags both directions of registry/worktree-dir drift: a
// registry entry whose worktree directory no longer exists (✗ — `ct list`
// and friends would misbehave against a slot with nothing behind it), and a
// directory inside worktrees_dir that isn't registered (⚠ — e.g. `git
// worktree add` run by hand, bypassing `ct create`). Symlinked
// files.symlink_siblings entries are never flagged: os.ReadDir reports a
// symlink's own entry type, so IsDir() is false for them regardless of what
// they point at.
func checkOrphanSlots(cfg *config.Config, reg *slots.Registry) doctorGroup {
	registered := reg.Slots()

	var lines []doctorLine
	for _, name := range sortedRegistryNames(reg) {
		if _, err := os.Stat(filepath.Join(cfg.WorktreesDir, name)); errors.Is(err, os.ErrNotExist) {
			lines = append(lines, doctorLine{sevFail, fmt.Sprintf("orphan slot %d (%s) — run ct remove %s", registered[name], name, name)})
		}
	}

	entries, err := os.ReadDir(cfg.WorktreesDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		lines = append(lines, doctorLine{sevFail, fmt.Sprintf("read worktrees dir: %v", err)})
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := registered[e.Name()]; !ok {
			lines = append(lines, doctorLine{sevWarn, fmt.Sprintf("unregistered dir %s", e.Name())})
		}
	}

	if len(lines) == 0 {
		lines = []doctorLine{{sevOK, "no orphan slots"}}
	}
	return doctorGroup{title: "orphan slots", lines: lines}
}

// checkHooks verifies every configured hook path exists and is executable.
// An unconfigured hook (empty path) is a no-op, same as internal/hooks.Run.
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

// checkHookPath reports whether the hook at absPath (already resolved by
// hookAbsPath; "" means unconfigured) exists and has an executable bit set.
// ok is true when there's nothing to report.
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

// checkDNS flags, for every registered worktree, a missing /etc/hosts
// entry — as a ⚠, not a ✗: `ct hosts` already treats an unmanaged line
// bound to the same dns as good enough, and DNS is optional infrastructure
// (create still succeeds without it). Skipped entirely when dns_pattern is
// unset — there's nothing to check.
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

// sortedRegistryNames returns reg's registered worktree names, sorted, so
// every check's output order is stable.
func sortedRegistryNames(reg *slots.Registry) []string {
	slotByName := reg.Slots()
	names := make([]string, 0, len(slotByName))
	for name := range slotByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// anyFailed reports whether any line across every group is a ✗ — the sole
// condition under which `ct doctor` exits 1.
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

// printDoctorGroups renders groups as plain grouped lines — not a table:
// one "<title>:" header per group, its lines indented 2 spaces with a
// leading ✓/✗/⚠ mark, and one blank line between groups.
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
