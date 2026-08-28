package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/config"
)

func TestPortValues_GIVEN_baseAndStep_WHEN_computedForSlot_THEN_baseTPlusSlotTimesStep(t *testing.T) {
	cfg := &config.Config{
		Ports: map[string]config.Port{
			"DB_PORT":    {Base: intp(3306), Step: 10},
			"HTTPS_PORT": {Base: intp(10443), Step: 100},
		},
	}

	got := cfg.PortValues(3)

	if got["DB_PORT"] != 3336 {
		t.Fatalf("got DB_PORT %d, want 3336", got["DB_PORT"])
	}
	if got["HTTPS_PORT"] != 10743 {
		t.Fatalf("got HTTPS_PORT %d, want 10743", got["HTTPS_PORT"])
	}
}

func TestRenderEnv_GIVEN_portsAndEnvAndSlot_WHEN_rendered_THEN_portsFirstThenTemplatedEnvThenNameAndSlot(t *testing.T) {
	cfg := &config.Config{
		RepoRoot: "/home/dev/myrepo",
		Ports: map[string]config.Port{
			"DB_PORT": {Base: intp(3306), Step: 10},
		},
		Env: map[string]string{
			"PHP_SERVER_NAME": "app-{name}",
		},
		DNSPattern: "local-{name}.dev.example.com",
	}

	got := cfg.RenderEnv("feature", 2)

	want := "DB_PORT=3326\n" +
		"PHP_SERVER_NAME=app-feature\n" +
		"CT_NAME=feature\n" +
		"CT_SLOT=2\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderEnv_GIVEN_envValueReferencingDNS_WHEN_rendered_THEN_dnsPatternExpandedFirst(t *testing.T) {
	cfg := &config.Config{
		RepoRoot:   "/home/dev/myrepo",
		DNSPattern: "local-{name}.dev.example.com",
		Env: map[string]string{
			"REDIRECT_URI": "https://{dns}/callback",
		},
	}

	got := cfg.RenderEnv("feature", 1)

	want := "REDIRECT_URI=https://local-feature.dev.example.com/callback\n" +
		"CT_NAME=feature\n" +
		"CT_SLOT=1\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderEnv_GIVEN_multipleKeys_WHEN_rendered_THEN_sortedAlphabeticallyWithinEachGroup(t *testing.T) {
	cfg := &config.Config{
		RepoRoot: "/home/dev/myrepo",
		Ports: map[string]config.Port{
			"ZETA_PORT":  {Base: intp(100), Step: 1},
			"ALPHA_PORT": {Base: intp(200), Step: 1},
		},
		Env: map[string]string{
			"ZETA_ENV":  "z",
			"ALPHA_ENV": "a",
		},
	}

	got := cfg.RenderEnv("feature", 1)

	wantOrder := []string{"ALPHA_PORT=", "ZETA_PORT=", "ALPHA_ENV=", "ZETA_ENV=", "CT_NAME=", "CT_SLOT="}
	lastIdx := -1
	for _, want := range wantOrder {
		idx := strings.Index(got, want)
		if idx < 0 {
			t.Fatalf("expected %q to appear in %q", want, got)
		}
		if idx < lastIdx {
			t.Fatalf("expected %q to appear after index %d, got %d (full: %q)", want, lastIdx, idx, got)
		}
		lastIdx = idx
	}
}

func TestWriteEnv_GIVEN_mainEnvExists_WHEN_written_THEN_inheritedMinusOverriddenKeysPlusLabeledCtBlock(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, ".env"), []byte(
		"PINGCODE=99977\n"+
			"DB_PORT=3306\n"+ // ct writes DB_PORT itself -> must be stripped
			"MACHINE_DNS=local.dev.example.com\n",
	), 0o644); err != nil {
		t.Fatalf("WriteFile main .env: %v", err)
	}

	cfg := &config.Config{
		RepoRoot: repoRoot,
		Ports: map[string]config.Port{
			"DB_PORT": {Base: intp(3306), Step: 10},
		},
	}

	dir := t.TempDir()
	if err := cfg.WriteEnv(dir, "feature", 2); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(data)

	if !strings.Contains(got, "PINGCODE=99977") {
		t.Fatalf("expected inherited PINGCODE line, got:\n%s", got)
	}
	if !strings.Contains(got, "MACHINE_DNS=local.dev.example.com") {
		t.Fatalf("expected inherited MACHINE_DNS line, got:\n%s", got)
	}
	if strings.Contains(got, "DB_PORT=3306") {
		t.Fatalf("expected the inherited DB_PORT=3306 line to be stripped (ct overrides it), got:\n%s", got)
	}
	if !strings.Contains(got, "# ── ct (slot 2) ─────────────") {
		t.Fatalf("expected the ct block header, got:\n%s", got)
	}
	if !strings.Contains(got, "DB_PORT=3326") {
		t.Fatalf("expected the ct-computed DB_PORT=3326, got:\n%s", got)
	}

	headerIdx := strings.Index(got, "# ── ct")
	pingcodeIdx := strings.Index(got, "PINGCODE")
	dbPortIdx := strings.Index(got, "DB_PORT=3326")
	if !(pingcodeIdx < headerIdx && headerIdx < dbPortIdx) {
		t.Fatalf("expected order: inherited content, then header, then ct block; got:\n%s", got)
	}
}

func TestWriteEnv_GIVEN_noMainEnv_WHEN_written_THEN_onlyCtBlockNoHeader(t *testing.T) {
	repoRoot := t.TempDir() // no .env here

	cfg := &config.Config{
		RepoRoot: repoRoot,
		Ports: map[string]config.Port{
			"DB_PORT": {Base: intp(3306), Step: 10},
		},
	}

	dir := t.TempDir()
	if err := cfg.WriteEnv(dir, "feature", 1); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "# ── ct") {
		t.Fatalf("did not expect a boundary header with no main .env to label a boundary against, got:\n%s", data)
	}
	if string(data) != cfg.RenderEnv("feature", 1) {
		t.Fatalf("got %q, want exactly the ct block", data)
	}
}

func TestWriteEnv_GIVEN_worktreeDir_WHEN_written_THEN_dotEnvFileCreatedWithRenderedContent(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		RepoRoot: "/home/dev/myrepo",
		Ports: map[string]config.Port{
			"DB_PORT": {Base: intp(3306), Step: 10},
		},
	}

	if err := cfg.WriteEnv(dir, "feature", 2); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := cfg.RenderEnv("feature", 2)
	if string(data) != want {
		t.Fatalf("got %q, want %q", string(data), want)
	}
}
