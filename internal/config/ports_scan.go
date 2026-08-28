package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// scaffoldPort is one discovered ports: entry: Base is nil when no value
// could be resolved anywhere (bare ${VAR}, nothing in .env) — the scaffold
// still emits it (as `base: null`) so the human sees every var compose
// actually uses. Comment holds zero or more full comment lines (warnings,
// "set me" notes, collision notes) rendered directly above the entry.
type scaffoldPort struct {
	Name    string
	Base    *int
	Step    int
	Comment []string
}

var (
	reVarColonDefault = regexp.MustCompile(`^\$\{(\w+):-(\d+)\}$`)
	reVarDashDefault  = regexp.MustCompile(`^\$\{(\w+)-(\d+)\}$`)
	reVarBare         = regexp.MustCompile(`^\$\{(\w+)\}$`)
	reLiteral         = regexp.MustCompile(`^(\d+)$`)
	// reIPPrefix strips an optional bind-address prefix ahead of the real
	// host spec: either a literal IPv4 dotted quad ("127.0.0.1:...") or —
	// as real compose files commonly write it — a ${VAR}/${VAR:-d}/${VAR-d}
	// expression standing in for the bind address itself (TagPay's
	// "${BIND_IP:-0.0.0.0}:${DB_PORT-3306}:3306" is exactly this shape).
	reIPPrefix = regexp.MustCompile(`^(?:\$\{\w+(?::?-[^}]*)?\}|\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):(.+)$`)
)

// flexString captures a YAML scalar's raw text regardless of whether it was
// written as a quoted string or a bare int (e.g. compose's `published:`).
type flexString string

func (s *flexString) UnmarshalYAML(node *yaml.Node) error {
	*s = flexString(node.Value)
	return nil
}

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Ports []yaml.Node `yaml:"ports"`
}

// composeServiceRaw captures a service's `ports:` value as a raw Node
// (undecoded), so the fallback merger can inspect its `!override` tag
// before deciding how to combine it with an earlier file's declaration.
type composeServiceRaw struct {
	PortsNode yaml.Node `yaml:"ports"`
}

type composeFileRaw struct {
	Services map[string]composeServiceRaw `yaml:"services"`
}

type composePortLong struct {
	Target    int        `yaml:"target"`
	Published flexString `yaml:"published"`
}

// rawBinding is one host-binding, parsed into its grammar pieces but not
// yet resolved against .env: varName is "" for a literal numeric binding;
// hasDefault distinguishes `${VAR:-d}`/`${VAR-d}` (literal holds d) from a
// bare `${VAR}` (literal is meaningless, containerPort is the only number
// available). svcName is the owning compose service — used to group
// literal bindings into one "cannot be replicated" comment per service.
// sourceFile is the originating compose filename when the discovery path
// tracks it per-binding (fallback yaml merge); it is "" for docker compose
// config's already-merged output, where no single source file can be
// named per binding.
type rawBinding struct {
	varName       string
	literal       int
	hasDefault    bool
	containerPort int
	svcName       string
	sourceFile    string
}

// scanComposePorts discovers every host-port binding for root's compose
// files, resolves each ${VAR} binding against <root>/.env into one
// scaffoldPort per distinct var (collision-checked across slots
// 1..maxSlots), and separately groups literal (non-parameterized)
// bindings into one "cannot be replicated" comment per owning service
// (literalComments — clone-tree never rewrites compose YAML, so a literal
// binding cannot be made slot-aware; it gets no ports: entry and no .env
// var, only a comment). discovery names which path produced the bindings
// ("docker compose config", "fallback yaml merge", or "none" when there
// are no compose files), purely informational (logged by the caller /
// returned for tests).
func scanComposePorts(root string, maxSlots int) (ports []scaffoldPort, literalComments []string, discovery string, err error) {
	files, err := composeFiles(root)
	if err != nil {
		return nil, nil, "", err
	}
	if len(files) == 0 {
		return nil, nil, "none", nil
	}

	dotenv, _ := parseDotEnv(filepath.Join(root, ".env"))

	raws, discovery := extractBindings(root, files)
	return resolvePorts(raws, dotenv, maxSlots), literalPortComments(raws), discovery, nil
}

// composeFiles resolves the file list docker compose itself would use:
// <root>/.env's COMPOSE_FILE (split on COMPOSE_PATH_SEPARATOR, default ":")
// when set, else the first compose default found plus its matching
// override file. Only existing files are returned.
func composeFiles(root string) ([]string, error) {
	dotenv, _ := parseDotEnv(filepath.Join(root, ".env"))
	if raw, ok := dotenv["COMPOSE_FILE"]; ok && raw != "" {
		sep := dotenv["COMPOSE_PATH_SEPARATOR"]
		if sep == "" {
			sep = ":"
		}
		var files []string
		for _, part := range strings.Split(raw, sep) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !filepath.IsAbs(part) {
				part = filepath.Join(root, part)
			}
			if _, statErr := os.Stat(part); statErr == nil {
				files = append(files, part)
			}
		}
		return files, nil
	}
	return defaultComposeFiles(root)
}

// defaultComposeFiles picks the first existing compose base file in
// docker compose's own precedence order, plus its matching
// "<base>.override.<ext>" when present.
func defaultComposeFiles(root string) ([]string, error) {
	var base string
	for _, candidate := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(root, candidate)); err == nil {
			base = candidate
			break
		}
	}
	if base == "" {
		return nil, nil
	}

	files := []string{filepath.Join(root, base)}
	ext := filepath.Ext(base)
	override := filepath.Join(root, strings.TrimSuffix(base, ext)+".override"+ext)
	if _, err := os.Stat(override); err == nil {
		files = append(files, override)
	}
	return files, nil
}

// runDockerComposeConfig is overridable in tests to force the fallback
// path without depending on whether docker is installed on the host
// running the tests.
var runDockerComposeConfig = func(root string, files []string) ([]byte, error) {
	args := []string{"compose"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, "config", "--no-interpolate")
	cmd := exec.Command("docker", args...)
	cmd.Dir = root
	return cmd.Output()
}

// extractBindings runs the primary discovery path (docker compose config,
// which merges COMPOSE_FILE/override/!override exactly like a real `up`
// would) and falls back to clone-tree's own yaml.v3 merge when docker
// compose is unavailable or errors. Which path ran is logged to stderr and
// returned for callers that want to report it.
func extractBindings(root string, files []string) ([]rawBinding, string) {
	if out, err := runDockerComposeConfig(root, files); err == nil {
		if raws, perr := parseComposeDoc(out); perr == nil {
			_, _ = fmt.Fprintln(os.Stderr, "ct: port discovery via docker compose config --no-interpolate")
			return raws, "docker compose config"
		}
	}
	_, _ = fmt.Fprintln(os.Stderr, "ct: port discovery via fallback yaml merge (docker compose unavailable or failed)")
	return extractBindingsFallback(files), "fallback yaml merge"
}

// parseComposeDoc extracts raw bindings from a single already-merged
// compose document (docker compose config's stdout).
func parseComposeDoc(data []byte) ([]rawBinding, error) {
	var doc composeFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var raws []rawBinding
	for _, svcName := range sortedServiceNames(doc.Services) {
		for _, node := range doc.Services[svcName].Ports {
			binding, ok := extractBinding(node)
			if !ok {
				continue
			}
			// "" for sourceFile: this is docker compose config's
			// already-merged output, so no single file can be named
			// per binding.
			rb, ok := parseBindingString(binding, "")
			if !ok {
				continue
			}
			rb.svcName = svcName
			raws = append(raws, rb)
		}
	}
	return raws, nil
}

// extractBindingsFallback merges each file's per-service ports declaration
// in file order — a later file's `!override`-tagged ports list replaces
// the running one for that service, otherwise it is appended — then
// extracts raw bindings from the merged result. Malformed or unreadable
// files are skipped, same as the old single-file scan.
func extractBindingsFallback(files []string) []rawBinding {
	type portItem struct {
		node *yaml.Node
		file string
	}
	merged := map[string][]portItem{}
	var order []string

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var doc composeFileRaw
		if err := yaml.Unmarshal(data, &doc); err != nil {
			continue
		}
		for _, svcName := range sortedRawServiceNames(doc.Services) {
			portsNode := doc.Services[svcName].PortsNode
			if portsNode.Kind == 0 {
				continue // this file doesn't touch this service's ports
			}
			if _, exists := merged[svcName]; !exists {
				order = append(order, svcName)
			}
			items := make([]portItem, len(portsNode.Content))
			for i, item := range portsNode.Content {
				items[i] = portItem{node: item, file: filepath.Base(f)}
			}
			if portsNode.Tag == "!override" || merged[svcName] == nil {
				merged[svcName] = items
			} else {
				merged[svcName] = append(merged[svcName], items...)
			}
		}
	}

	sort.Strings(order)
	var raws []rawBinding
	for _, svcName := range order {
		for _, item := range merged[svcName] {
			binding, ok := extractBinding(*item.node)
			if !ok {
				continue
			}
			rb, ok := parseBindingString(binding, item.file)
			if !ok {
				continue
			}
			rb.svcName = svcName
			raws = append(raws, rb)
		}
	}
	return raws
}

// uniqueSortedInts returns the distinct values of nums, ascending.
func uniqueSortedInts(nums []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, n := range nums {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func sortedServiceNames(services map[string]composeService) []string {
	names := make([]string, 0, len(services))
	for n := range services {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func sortedRawServiceNames(services map[string]composeServiceRaw) []string {
	names := make([]string, 0, len(services))
	for n := range services {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// extractBinding returns the raw host-binding string for a ports list
// entry: the whole scalar for short syntax ("8080:80"), or
// "<published>:<target>" reassembled for long syntax
// ({target: 80, published: "8080"}).
func extractBinding(node yaml.Node) (string, bool) {
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value, true
	case yaml.MappingNode:
		var long composePortLong
		if err := node.Decode(&long); err != nil {
			return "", false
		}
		if long.Published == "" || long.Target == 0 {
			return "", false
		}
		return string(long.Published) + ":" + strconv.Itoa(long.Target), true
	default:
		return "", false
	}
}

// parseBindingString parses a "[ip:]host:container[/proto]" binding string
// into its grammar pieces. The container port is always the last segment
// (stripped of an optional /proto suffix); an optional literal IPv4 prefix
// on the host part is stripped before classifying the host as one of the
// three recognized forms.
func parseBindingString(binding, sourceFile string) (rawBinding, bool) {
	idx := strings.LastIndex(binding, ":")
	if idx < 0 {
		return rawBinding{}, false
	}
	hostPart := binding[:idx]
	if m := reIPPrefix.FindStringSubmatch(hostPart); m != nil {
		hostPart = m[1]
	}
	containerPart := strings.SplitN(binding[idx+1:], "/", 2)[0]
	containerPort, err := strconv.Atoi(containerPart)
	if err != nil {
		return rawBinding{}, false
	}

	switch {
	case reVarColonDefault.MatchString(hostPart):
		m := reVarColonDefault.FindStringSubmatch(hostPart)
		n, _ := strconv.Atoi(m[2])
		return rawBinding{varName: m[1], literal: n, hasDefault: true, containerPort: containerPort, sourceFile: sourceFile}, true
	case reVarDashDefault.MatchString(hostPart):
		m := reVarDashDefault.FindStringSubmatch(hostPart)
		n, _ := strconv.Atoi(m[2])
		return rawBinding{varName: m[1], literal: n, hasDefault: true, containerPort: containerPort, sourceFile: sourceFile}, true
	case reVarBare.MatchString(hostPart):
		m := reVarBare.FindStringSubmatch(hostPart)
		return rawBinding{varName: m[1], containerPort: containerPort, sourceFile: sourceFile}, true
	case reLiteral.MatchString(hostPart):
		n, _ := strconv.Atoi(hostPart)
		return rawBinding{literal: n, containerPort: containerPort, sourceFile: sourceFile}, true
	default:
		return rawBinding{}, false
	}
}

// resolvePorts resolves every ${VAR} raw binding against dotenv (first
// occurrence wins per var name — later duplicates, e.g. the same var bound
// by two services, are dropped), then runs the cross-slot collision pass.
// Literal (non-parameterized) bindings never reach resolveOne — they get
// no ports: entry at all; see literalPortComments for their handling.
func resolvePorts(raws []rawBinding, dotenv map[string]string, maxSlots int) []scaffoldPort {
	var ports []scaffoldPort
	seen := map[string]bool{}
	for _, rb := range raws {
		if rb.varName == "" {
			continue
		}
		p := resolveOne(rb, dotenv)
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		ports = append(ports, p)
	}
	resolveCollisions(ports, maxSlots, literalPorts(raws))
	return ports
}

// literalPorts returns the distinct literal (non-parameterized) host ports
// among raws. clone-tree never rewrites compose YAML, so these are bound
// identically by main and every worktree — permanent obstacles the
// collision pass must also steer var series away from.
func literalPorts(raws []rawBinding) []int {
	var lits []int
	for _, rb := range raws {
		if rb.varName == "" {
			lits = append(lits, rb.literal)
		}
	}
	return uniqueSortedInts(lits)
}

// literalPortComments groups literal (non-parameterized) host-port
// bindings by owning service — clone-tree never rewrites compose YAML, so
// these can never be made slot-aware — and renders one "cannot be
// replicated" comment per service listing every distinct literal host
// port on that service, sorted ascending. The source filename is appended
// only when known: the fallback yaml-merge path tracks it per-binding: the
// docker compose config path does not (its output is already merged), so
// no filename is printed for it.
func literalPortComments(raws []rawBinding) []string {
	type group struct {
		ports []int
		file  string
	}
	groups := map[string]*group{}
	var order []string
	for _, rb := range raws {
		if rb.varName != "" {
			continue
		}
		g, ok := groups[rb.svcName]
		if !ok {
			g = &group{}
			groups[rb.svcName] = g
			order = append(order, rb.svcName)
		}
		g.ports = append(g.ports, rb.literal)
		if g.file == "" {
			g.file = rb.sourceFile
		}
	}
	sort.Strings(order)

	comments := make([]string, 0, len(order))
	for _, svc := range order {
		g := groups[svc]
		ports := uniqueSortedInts(g.ports)
		portStrs := make([]string, len(ports))
		for i, p := range ports {
			portStrs[i] = strconv.Itoa(p)
		}
		comment := fmt.Sprintf("# service %s cannot be replicated: non-parameterized host port %s", svc, strings.Join(portStrs, ", "))
		if g.file != "" {
			comment += fmt.Sprintf(" (%s)", g.file)
		}
		comments = append(comments, comment)
	}
	return comments
}

// resolveOne resolves one ${VAR} raw binding to its value per the
// scaffold's two-way rule: ${VAR:-d}/${VAR-d} -> .env[VAR] ?? d; bare
// ${VAR} -> .env[VAR], else unresolved (base: null + a "set me" comment).
func resolveOne(rb rawBinding, dotenv map[string]string) scaffoldPort {
	if !rb.hasDefault {
		if v, ok := dotenvInt(dotenv, rb.varName); ok {
			base, step := baseStep(v)
			return scaffoldPort{Name: rb.varName, Base: &base, Step: step}
		}
		// Nothing tells us what this port actually is; step is uniform
		// (10) regardless, so there is nothing left to derive from the
		// container port here.
		comment := fmt.Sprintf("# set me: ${%s} has no value in .env", rb.varName)
		return scaffoldPort{Name: rb.varName, Step: 10, Comment: []string{comment}}
	}

	value := rb.literal
	if v, ok := dotenvInt(dotenv, rb.varName); ok {
		value = v
	}
	base, step := baseStep(value)
	return scaffoldPort{Name: rb.varName, Base: &base, Step: step}
}

// baseStep is the base/step rule: a port already >= 1024 keeps its value
// as base; a port < 1024 (a well-known/privileged port a proxy maps in
// from) is pushed into the 10000+ range instead, since worktree slots
// cannot bind privileged ports. Step is uniform (10) for every var — the
// collision pass is what actually keeps series apart, not a wider default
// step.
func baseStep(original int) (base, step int) {
	if original >= 1024 {
		return original, 10
	}
	return 10000 + original, 10
}

// maxCollisionRounds caps the fixpoint loop below; it is a generous safety
// net, not a tuning knob — every var can only be bumped maxCollisionBumps
// times, so the loop is guaranteed to stabilize well before this is hit.
const maxCollisionRounds = 50

// resolveCollisions repeatedly scans every ordered pair of resolved ports:
// if var i's own worktree-slot values (slots 1..maxSlots — never its own
// base, which is pinned to whatever main/slot-0 actually uses) land on var
// j's base or on any of j's worktree-slot values, i is the "mover" that
// created the collision, so i's step is the one multiplied by 10 (j's base
// is fixed; touching j's step wouldn't remove a hit on j's base anyway).
// literalPorts (non-parameterized host-port bindings) are checked the same
// way — they are bound identically by main and every worktree, so they are
// permanent obstacles too, but since they are not entries they never get
// bumped themselves. This can cascade — bumping i can newly collide with a
// var k (or a literal) that was clean before — hence the fixpoint loop, not
// a single pass. A var capped at maxCollisionBumps without becoming clean
// gets a "# collision" comment instead of silently shipping an overlap.
func resolveCollisions(ports []scaffoldPort, maxSlots int, literalPorts []int) {
	bumps := make([]int, len(ports))
	commented := make([]bool, len(ports))
	literalSet := make(map[int]bool, len(literalPorts))
	for _, lp := range literalPorts {
		literalSet[lp] = true
	}

	bumpOrComment := func(i int) {
		if bumps[i] >= maxCollisionBumps {
			if !commented[i] {
				ports[i].Comment = append(ports[i].Comment,
					fmt.Sprintf("# collision: still overlaps another var's ports after %d step bumps — adjust base/step manually", maxCollisionBumps))
				commented[i] = true
			}
			return
		}
		ports[i].Step *= 10
		bumps[i]++
	}

	for round := 0; round < maxCollisionRounds; round++ {
		startBumps := append([]int{}, bumps...)
		for i := range ports {
			if ports[i].Base == nil {
				continue
			}
			for j := range ports {
				if i == j || ports[j].Base == nil {
					continue
				}
				if movesInto(ports[i], ports[j], maxSlots) {
					bumpOrComment(i)
				}
			}
			if hitsLiteral(ports[i], maxSlots, literalSet) {
				bumpOrComment(i)
			}
		}
		changed := false
		for i := range bumps {
			if bumps[i] != startBumps[i] {
				changed = true
				break
			}
		}
		if !changed {
			return
		}
	}
}

// movesInto reports whether pi's own worktree-slot values (1..maxSlots)
// land on pj's base or on any of pj's worktree-slot values.
func movesInto(pi, pj scaffoldPort, maxSlots int) bool {
	pjPoints := map[int]bool{*pj.Base: true}
	for s := 1; s <= maxSlots; s++ {
		pjPoints[*pj.Base+s*pj.Step] = true
	}
	for s := 1; s <= maxSlots; s++ {
		if pjPoints[*pi.Base+s*pi.Step] {
			return true
		}
	}
	return false
}

// hitsLiteral reports whether pi's own worktree-slot values (1..maxSlots)
// land on any literal (non-parameterized) host port. pi's base is never
// checked — a literal obstacle only matters for the slots worktrees
// actually use.
func hitsLiteral(pi scaffoldPort, maxSlots int, literalSet map[int]bool) bool {
	for s := 1; s <= maxSlots; s++ {
		if literalSet[*pi.Base+s*pi.Step] {
			return true
		}
	}
	return false
}

// parseDotEnv parses a .env file: KEY=VALUE lines, blank lines and #
// comments ignored, surrounding single/double quotes on the value
// stripped. A missing file yields an empty map (not an error) — callers
// treat "no .env" as "no overrides", never as a failure.
func parseDotEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}, err
	}

	result := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		result[strings.TrimSpace(key)] = unquoteDotEnvValue(strings.TrimSpace(value))
	}
	return result, nil
}

func unquoteDotEnvValue(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func dotenvInt(dotenv map[string]string, key string) (int, bool) {
	v, ok := dotenv[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}
