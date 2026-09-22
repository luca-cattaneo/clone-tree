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

type scaffoldPort struct {
	Name    string
	Base    *int
	Step    int
	Comment []string
	ContainerPort int
	Service       string
}

var (
	reVarColonDefault = regexp.MustCompile(`^\$\{(\w+):-(\d+)\}$`)
	reVarDashDefault  = regexp.MustCompile(`^\$\{(\w+)-(\d+)\}$`)
	reVarBare         = regexp.MustCompile(`^\$\{(\w+)\}$`)
	reLiteral         = regexp.MustCompile(`^(\d+)$`)
	// reIPPrefix strips an optional bind-address prefix ahead of the real
	// host spec: either a literal IPv4 dotted quad ("127.0.0.1:...") or
	// expression standing in for the bind address itself
	// ("${BIND_IP:-0.0.0.0}:${DB_PORT-3306}:3306").
	reIPPrefix = regexp.MustCompile(`^(?:\$\{\w+(?::?-[^}]*)?\}|\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):(.+)$`)
)

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

type rawBinding struct {
	varName       string
	literal       int
	hasDefault    bool
	containerPort int
	svcName       string
	sourceFile    string
}

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

type LiteralBinding struct {
	Service string
	Port    int
}

func ScanLiteralPortBindings(root string) ([]LiteralBinding, error) {
	files, err := composeFiles(root)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	raws, _ := extractBindings(root, files)
	var out []LiteralBinding
	for _, rb := range raws {
		if rb.varName == "" {
			out = append(out, LiteralBinding{Service: rb.svcName, Port: rb.literal})
		}
	}
	return out, nil
}

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
// docker compose's own precedence order.
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
// would) and falls back to clone-tree's own yaml.
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
// into its grammar pieces.
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

func literalPorts(raws []rawBinding) []int {
	var lits []int
	for _, rb := range raws {
		if rb.varName == "" {
			lits = append(lits, rb.literal)
		}
	}
	return uniqueSortedInts(lits)
}

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

func resolveOne(rb rawBinding, dotenv map[string]string) scaffoldPort {
	if !rb.hasDefault {
		if v, ok := dotenvInt(dotenv, rb.varName); ok {
			base, step := baseStep(v)
			return scaffoldPort{Name: rb.varName, Base: &base, Step: step, ContainerPort: rb.containerPort, Service: rb.svcName}
		}
		comment := fmt.Sprintf("# set me: ${%s} has no value in .env", rb.varName)
		return scaffoldPort{Name: rb.varName, Step: 10, Comment: []string{comment}, ContainerPort: rb.containerPort, Service: rb.svcName}
	}

	value := rb.literal
	if v, ok := dotenvInt(dotenv, rb.varName); ok {
		value = v
	}
	base, step := baseStep(value)
	return scaffoldPort{Name: rb.varName, Base: &base, Step: step, ContainerPort: rb.containerPort, Service: rb.svcName}
}

func baseStep(original int) (base, step int) {
	if original >= 1024 {
		return original, 10
	}
	return 10000 + original, 10
}

const maxCollisionRounds = 50

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

func hitsLiteral(pi scaffoldPort, maxSlots int, literalSet map[int]bool) bool {
	for s := 1; s <= maxSlots; s++ {
		if literalSet[*pi.Base+s*pi.Step] {
			return true
		}
	}
	return false
}

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
