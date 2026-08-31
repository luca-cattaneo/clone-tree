// Package config loads, validates, and templates .clone-tree/config.yaml.
// LoadExisting never scaffolds; a caller that wants to auto-scaffold a
// missing config by scanning the repo calls ScaffoldOrError on
// ErrNoConfig (see scaffold.go).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Port is one env-var -> host-port mapping: the concrete port for slot N is
// Base + N*Step. Base is a pointer because the scaffold can discover a bare
// ${VAR} binding with no resolvable value anywhere (no default in the
// compose file, no entry in .env) — it writes `base: null` and leaves it to
// the user to fill in; Validate rejects a nil/zero Base with a message
// naming the var.
type Port struct {
	Base *int `yaml:"base"`
	Step int  `yaml:"step"`
}

// CloneCoW is a copy-on-write clone source/destination pair; Src/Dst are
// templated (see Expand) before being passed to internal/fsops.CloneCoW.
type CloneCoW struct {
	Src string `yaml:"src"`
	Dst string `yaml:"dst"`
}

// Files lists gitignored paths to materialize into a new worktree via
// internal/fsops, in order: IDE, Copy, Hardlink, CloneCoW, SymlinkSiblings.
type Files struct {
	// IDE has the same semantics as Copy (relative to repo root,
	// recursive, missing → warn+skip) but is kept as its own key so IDE
	// config dirs (.idea/.vscode/.fleet/.vs) — which typically need
	// per-worktree patching via a post_create hook — stay distinct from
	// generic copies.
	IDE             []string   `yaml:"ide"`
	Copy            []string   `yaml:"copy"`
	Hardlink        []string   `yaml:"hardlink"`
	CloneCoW        []CloneCoW `yaml:"clone_cow"`
	SymlinkSiblings []string   `yaml:"symlink_siblings"`
}

// Hooks are optional repo-local executables (paths relative to RepoRoot).
// Run by internal/hooks with the instance env contract (CT_NAME, CT_SLOT,
// CT_DNS, CT_WT_PATH, plus every port var) — see internal/hooks.Env.
type Hooks struct {
	PostCreate string `yaml:"post_create"`
	PreRemove  string `yaml:"pre_remove"`
}

// Config is the parsed, validated .clone-tree/config.yaml.
type Config struct {
	Version      int               `yaml:"version"`
	WorktreesDir string            `yaml:"worktrees_dir"`
	DNSPattern   string            `yaml:"dns_pattern"`
	MaxSlots     int               `yaml:"max_slots"`
	Ports        map[string]Port   `yaml:"ports"`
	Env          map[string]string `yaml:"env"`
	Files        Files             `yaml:"files"`
	Hooks        Hooks             `yaml:"hooks"`
	// Urls maps a human label (e.g. "Web Client") to a URL template
	// expanded the same way env: values are (config.Expand against
	// Config.InstanceVars — {name} {slot} {dns} {repo} {projects_dir} +
	// every port var). Rendered by `ct hosts <name|slot>` as a Service/URL
	// table underneath the DNS row. Empty (the default) renders nothing.
	Urls map[string]string `yaml:"urls"`

	// RepoRoot is not part of the YAML schema; Load stamps it so
	// downstream templating ({repo}, {projects_dir}) doesn't need to
	// re-derive the git root.
	RepoRoot string `yaml:"-"`
}

// ANSI escapes for ScaffoldedError's checklist. Kept local to config (not
// internal/cli's ansiBold/ansiReset) because cli imports config — importing
// cli here would create a cycle.
const (
	ansiBold   = "\033[1m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiReset  = "\033[0m"
)

// ScaffoldedError is returned by Load when no config existed for the repo:
// Load auto-scaffolded one and wrote it to Path, and stops there — the
// human must review the generated file (it is full of "set me"/"review &
// commit" comments) before ct can proceed. Error() renders a multi-line,
// colorized checklist (main.go prints it without the usual "ct: " prefix —
// see errors.As handling there) since a terse one-liner isn't enough to
// tell a first-time user that commented `# hardlink:`/`# ide:`/`# copy:`
// suggestions do nothing until uncommented.
type ScaffoldedError struct {
	Path string
}

func (e *ScaffoldedError) Error() string {
	return fmt.Sprintf(
		"%s🌱 Generated %s — ct stopped here on purpose.%s\n\n"+
			"Nothing runs until you review it:\n"+
			"  %s🔌 ports:%s   fix every `base: null  # set me` and every `# collision`; "+
			"`# cannot be replicated` = literal host port in compose, parameterize it as ${VAR:-port} or live with it\n"+
			"  %s📁 files:%s   everything under `# hardlink:` / `# ide:` / `# copy:` is a SUGGESTION — "+
			"commented out = ignored. Uncomment what a fresh worktree needs (.env, vendor/, .idea/…) and move it under `files:`\n"+
			"  %s🌐 dns_pattern:%s   empty = everything on localhost:<port>; set it (e.g. `\"{name}.myapp.localhost\"`) "+
			"only if the app pins its origin to a hostname (OAuth redirect URIs, cookie domain, server name)\n"+
			"  %s📝 env:%s   extra .env lines per worktree (e.g. APP_NAME: \"myapp-{name}\") — anything besides ports that must differ between worktrees\n"+
			"  %s⚙️ hooks:%s   optional, fill only if you need them\n\n"+
			"Then commit .clone-tree/config.yaml and re-run your command.",
		ansiBold, e.Path, ansiReset,
		ansiYellow, ansiReset,
		ansiCyan, ansiReset,
		ansiBold, ansiReset,
		ansiBold, ansiReset,
		ansiBold, ansiReset,
	)
}

// ErrNoConfig is returned by LoadExisting when neither overridePath nor
// <root>/.clone-tree/config.yaml exists. Only `ct create` and
// `ct create-config` react to it by auto-scaffolding (via ScaffoldOrError,
// see scaffold.go); every other command must surface it as a clean error
// (or, for `ct list`, fall back to a bare-mode Config) rather than generate
// a config.yaml as a side effect of a read-only command.
var ErrNoConfig = errors.New("config: no .clone-tree/config.yaml — run `ct create` to scaffold one, or pass --config")

// ResolvePath returns the config.yaml path LoadExisting resolves for root
// and overridePath, without reading or parsing it: overridePath verbatim
// when set (the --config escape hatch), else
// <root>/.clone-tree/config.yaml.
func ResolvePath(root, overridePath string) string {
	if overridePath != "" {
		return overridePath
	}
	return filepath.Join(root, ".clone-tree", "config.yaml")
}

// LoadExisting resolves the config for the repo rooted at root: overridePath
// first (the --config escape hatch), then <root>/.clone-tree/config.yaml.
// It never auto-scaffolds: a missing config (no override, no
// <root>/.clone-tree/config.yaml) returns ErrNoConfig instead of generating
// one. worktrees_dir is expanded and resolved to an absolute path before
// return; env/dns_pattern stay as raw templates, expanded per-instance at
// create time (see env.go). Callers that want to auto-scaffold on
// ErrNoConfig call ScaffoldOrError (see scaffold.go) themselves.
func LoadExisting(root, overridePath string) (*Config, error) {
	path := ResolvePath(root, overridePath)

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && overridePath == "" {
		return nil, ErrNoConfig
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg, err := parse(data)
	if err != nil {
		return nil, err
	}
	cfg.RepoRoot = root
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.resolveWorktreesDir()
	return cfg, nil
}

func parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// Validate checks the invariants the rest of ct relies on.
func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("config: unsupported version %d, want 1", c.Version)
	}
	if c.MaxSlots < 1 {
		return fmt.Errorf("config: max_slots must be >= 1, got %d", c.MaxSlots)
	}
	for name, p := range c.Ports {
		if p.Base == nil || *p.Base <= 0 {
			return fmt.Errorf("config: set base for %s in .clone-tree/config.yaml", name)
		}
		if p.Step <= 0 {
			return fmt.Errorf("config: ports.%s.step must be > 0, got %d", name, p.Step)
		}
	}
	return nil
}

// resolveWorktreesDir expands {repo}/{projects_dir} in worktrees_dir and
// resolves the result to an absolute path (relative results are joined
// against RepoRoot; already-absolute results are kept as-is).
func (c *Config) resolveWorktreesDir() {
	expanded := Expand(c.WorktreesDir, BaseVars(c.RepoRoot))
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(c.RepoRoot, expanded)
	}
	c.WorktreesDir = filepath.Clean(expanded)
}

// BaseVars returns the templating vars derivable from the repo root alone:
// {repo} (the repo dir's basename) and {projects_dir} (its parent).
func BaseVars(repoRoot string) map[string]string {
	return map[string]string{
		"repo":         filepath.Base(repoRoot),
		"projects_dir": filepath.Dir(repoRoot),
	}
}

// Expand replaces every {key} in s with vars[key], for every key present in
// vars. Placeholders with no matching key are left untouched.
func Expand(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}
