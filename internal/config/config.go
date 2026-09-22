package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Port struct {
	Base *int `yaml:"base"`
	Step int  `yaml:"step"`
}

type CloneCoW struct {
	Src string `yaml:"src"`
	Dst string `yaml:"dst"`
}

type Files struct {
	IDE             []string   `yaml:"ide"`
	Copy            []string   `yaml:"copy"`
	Hardlink        []string   `yaml:"hardlink"`
	CloneCoW        []CloneCoW `yaml:"clone_cow"`
	SymlinkSiblings []string   `yaml:"symlink_siblings"`
}

type Hooks struct {
	PostCreate string `yaml:"post_create"`
	PreRemove  string `yaml:"pre_remove"`
}

type Config struct {
	Version      int               `yaml:"version"`
	WorktreesDir string            `yaml:"worktrees_dir"`
	BaseBranch   string            `yaml:"base_branch"`
	DNSPattern   string            `yaml:"dns_pattern"`
	MaxSlots     int               `yaml:"max_slots"`
	Ports        map[string]Port   `yaml:"ports"`
	Env          map[string]string `yaml:"env"`
	Files        Files             `yaml:"files"`
	Hooks        Hooks             `yaml:"hooks"`
	// RepoRoot is not part of the YAML schema, it cache the calculated value to call urls
	RepoRoot string `yaml:"-"`
	// Maps a human label (e.g. "Web Client") to a URL template
    Urls map[string]string `yaml:"urls"`
}

const (
	ansiBold   = "\033[1m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiReset  = "\033[0m"
)

// ScaffoldedError is returned by Load when no config existed for the repo
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

var ErrNoConfig = errors.New("config: no .clone-tree/config.yaml — run `ct create` to scaffold one, or pass --config")

func ResolvePath(root, overridePath string) string {
	if overridePath != "" {
		return overridePath
	}
	return filepath.Join(root, ".clone-tree", "config.yaml")
}

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

func (c *Config) resolveWorktreesDir() {
	expanded := Expand(c.WorktreesDir, BaseVars(c.RepoRoot))
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(c.RepoRoot, expanded)
	}
	c.WorktreesDir = filepath.Clean(expanded)
}

func BaseVars(repoRoot string) map[string]string {
	return map[string]string{
		"repo":         filepath.Base(repoRoot),
		"projects_dir": filepath.Dir(repoRoot),
	}
}

func Expand(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}
