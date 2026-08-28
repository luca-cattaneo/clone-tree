package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/config"
)

func intp(n int) *int { return &n }

func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".clone-tree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoad_GIVEN_validConfig_WHEN_loaded_THEN_fieldsParsedAndWorktreesDirExpanded(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `
version: 1
worktrees_dir: ../{repo}-worktrees
dns_pattern: "local-{name}.dev.example.com"
max_slots: 5
ports:
  DB_PORT: {base: 3306, step: 10}
env:
  PHP_SERVER_NAME: "app-{name}"
`)

	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Version != 1 {
		t.Fatalf("got version %d, want 1", cfg.Version)
	}
	if cfg.MaxSlots != 5 {
		t.Fatalf("got max_slots %d, want 5", cfg.MaxSlots)
	}
	want := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees")
	if cfg.WorktreesDir != want {
		t.Fatalf("got worktrees_dir %q, want %q", cfg.WorktreesDir, want)
	}
	if *cfg.Ports["DB_PORT"].Base != 3306 || cfg.Ports["DB_PORT"].Step != 10 {
		t.Fatalf("got ports.DB_PORT %#v", cfg.Ports["DB_PORT"])
	}
	if cfg.Env["PHP_SERVER_NAME"] != "app-{name}" {
		t.Fatalf("got env.PHP_SERVER_NAME %q, want raw template preserved", cfg.Env["PHP_SERVER_NAME"])
	}
}

func TestLoad_GIVEN_absoluteWorktreesDir_WHEN_loaded_THEN_keptAsIs(t *testing.T) {
	root := t.TempDir()
	abs := t.TempDir()
	writeConfig(t, root, "version: 1\nworktrees_dir: "+abs+"\nmax_slots: 1\n")

	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WorktreesDir != filepath.Clean(abs) {
		t.Fatalf("got %q, want %q", cfg.WorktreesDir, abs)
	}
}

func TestLoad_GIVEN_configOverridePath_WHEN_loaded_THEN_usesThatFileInsteadOfDiscovery(t *testing.T) {
	root := t.TempDir()
	overrideDir := t.TempDir()
	overridePath := filepath.Join(overrideDir, "custom-config.yaml")
	if err := os.WriteFile(overridePath, []byte("version: 1\nworktrees_dir: /tmp/wt\nmax_slots: 3\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(root, overridePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxSlots != 3 {
		t.Fatalf("got max_slots %d, want 3", cfg.MaxSlots)
	}
}

func TestLoad_GIVEN_configOverridePathMissing_WHEN_loaded_THEN_errors(t *testing.T) {
	root := t.TempDir()

	if _, err := config.Load(root, filepath.Join(root, "nope.yaml")); err == nil {
		t.Fatalf("expected error for missing --config path")
	}
}

func TestValidate_GIVEN_invariantViolations_WHEN_validated_THEN_errors(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
	}{
		{
			name: "wrong version",
			cfg:  config.Config{Version: 2, MaxSlots: 1},
		},
		{
			name: "zero max_slots",
			cfg:  config.Config{Version: 1, MaxSlots: 0},
		},
		{
			name: "negative max_slots",
			cfg:  config.Config{Version: 1, MaxSlots: -1},
		},
		{
			name: "nil port base",
			cfg: config.Config{Version: 1, MaxSlots: 1, Ports: map[string]config.Port{
				"DB_PORT": {Base: nil, Step: 10},
			}},
		},
		{
			name: "zero port base",
			cfg: config.Config{Version: 1, MaxSlots: 1, Ports: map[string]config.Port{
				"DB_PORT": {Base: intp(0), Step: 10},
			}},
		},
		{
			name: "zero port step",
			cfg: config.Config{Version: 1, MaxSlots: 1, Ports: map[string]config.Port{
				"DB_PORT": {Base: intp(3306), Step: 0},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err == nil {
				t.Fatalf("expected Validate to error for %s", tt.name)
			}
		})
	}
}

func TestValidate_GIVEN_nilPortBase_WHEN_validated_THEN_errorNamesTheVar(t *testing.T) {
	cfg := config.Config{Version: 1, MaxSlots: 1, Ports: map[string]config.Port{
		"BUGGREGATOR_HTTP_PORT": {Base: nil, Step: 10},
	}}

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error")
	}
	want := "set base for BUGGREGATOR_HTTP_PORT in .clone-tree/config.yaml"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %q, want it to contain %q", err.Error(), want)
	}
}

func TestValidate_GIVEN_minimalValidConfig_WHEN_validated_THEN_noError(t *testing.T) {
	cfg := config.Config{Version: 1, MaxSlots: 1}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestExpand_GIVEN_multiplePlaceholders_WHEN_expanded_THEN_allSubstituted(t *testing.T) {
	got := config.Expand("local-{name}-{slot}.{repo}", map[string]string{
		"name": "feature", "slot": "2", "repo": "myrepo",
	})
	want := "local-feature-2.myrepo"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpand_GIVEN_unknownPlaceholder_WHEN_expanded_THEN_leftUntouched(t *testing.T) {
	got := config.Expand("{unknown}-{name}", map[string]string{"name": "feature"})
	want := "{unknown}-feature"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoad_GIVEN_filesIDEConfigured_WHEN_loaded_THEN_parsedSeparatelyFromCopy(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `
version: 1
worktrees_dir: ../{repo}-worktrees
max_slots: 1
files:
  ide: [.idea/]
  copy: [conf/local.php]
`)

	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.Files.IDE) != 1 || cfg.Files.IDE[0] != ".idea/" {
		t.Fatalf("got files.ide %#v, want [.idea/]", cfg.Files.IDE)
	}
	if len(cfg.Files.Copy) != 1 || cfg.Files.Copy[0] != "conf/local.php" {
		t.Fatalf("got files.copy %#v, want [conf/local.php]", cfg.Files.Copy)
	}
}

func TestBaseVars_GIVEN_repoRoot_WHEN_computed_THEN_repoAndProjectsDirDerived(t *testing.T) {
	vars := config.BaseVars("/home/dev/myrepo")
	if vars["repo"] != "myrepo" {
		t.Fatalf("got repo %q, want myrepo", vars["repo"])
	}
	if vars["projects_dir"] != "/home/dev" {
		t.Fatalf("got projects_dir %q, want /home/dev", vars["projects_dir"])
	}
}
