package config_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/config"
)

// newFixtureRepo creates a git repo in a fresh temp dir with an initial
// commit, so `git status --ignored` works against it.
func newFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial commit")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func readGeneratedConfig(t *testing.T, repo string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, ".clone-tree", "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile generated config: %v", err)
	}
	return string(data)
}

// scaffoldAndParse scaffolds repo, then parses+validates the generated
// config.yaml via LoadExisting against the written path (an explicit
// --config-style override) — Scaffold itself now stops at "written to
// disk", so tests that need the parsed Config go through this instead.
func scaffoldAndParse(t *testing.T, repo string) *config.Config {
	t.Helper()
	path, err := config.Scaffold(repo)
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	cfg, err := config.LoadExisting(repo, path)
	if err != nil {
		t.Fatalf("LoadExisting generated config: %v", err)
	}
	return cfg
}

func TestScaffold_GIVEN_noComposeFile_WHEN_scaffolded_THEN_emptyPortsMapStillValid(t *testing.T) {
	repo := newFixtureRepo(t)

	cfg := scaffoldAndParse(t, repo)
	if len(cfg.Ports) != 0 {
		t.Fatalf("got ports %#v, want empty", cfg.Ports)
	}
	if cfg.Version != 1 || cfg.MaxSlots != 9 {
		t.Fatalf("got version=%d max_slots=%d, want 1/9", cfg.Version, cfg.MaxSlots)
	}
}

func TestScaffold_GIVEN_composeWithColonDashDefaultBinding_WHEN_scaffolded_THEN_reusesVarNameAndDefaultAsBase(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${PROXY_HTTP_PORT:-8080}:80"
`)

	cfg := scaffoldAndParse(t, repo)

	p, ok := cfg.Ports["PROXY_HTTP_PORT"]
	if !ok {
		t.Fatalf("expected PROXY_HTTP_PORT in ports, got %#v", cfg.Ports)
	}
	if p.Base == nil || *p.Base != 8080 || p.Step != 10 {
		t.Fatalf("got %#v, want base=8080 step=10 (>=1024 keeps original)", p)
	}
}

func TestScaffold_GIVEN_composeWithDashOnlyDefaultBinding_WHEN_scaffolded_THEN_sameResultAsColonDash(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${PROXY_HTTP_PORT-8080}:80"
`)

	cfg := scaffoldAndParse(t, repo)

	p, ok := cfg.Ports["PROXY_HTTP_PORT"]
	if !ok || p.Base == nil || *p.Base != 8080 {
		t.Fatalf("got %#v, want PROXY_HTTP_PORT base=8080", cfg.Ports)
	}
}

func TestScaffold_GIVEN_composeWithBareVarBindingAndNoEnvValue_WHEN_scaffolded_THEN_baseNullWithSetMeComment(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${PROXY_HTTP_PORT}:80"
`)

	// An unresolved var makes the generated config itself invalid (see
	// config.Validate), but Scaffold only writes the file — validation
	// happens on the next Load, covered separately.
	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "# set me: ${PROXY_HTTP_PORT} has no value in .env") {
		t.Fatalf("expected 'set me' comment, got:\n%s", data)
	}
	if !strings.Contains(data, "base: null") {
		t.Fatalf("expected 'base: null' in generated YAML, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_composeWithBareVarBindingAndEnvValue_WHEN_scaffolded_THEN_envValueUsedAsBase(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${PROXY_HTTP_PORT}:80"
`)
	writeFile(t, filepath.Join(repo, ".env"), "PROXY_HTTP_PORT=10080\n")

	cfg := scaffoldAndParse(t, repo)

	p, ok := cfg.Ports["PROXY_HTTP_PORT"]
	if !ok || p.Base == nil || *p.Base != 10080 {
		t.Fatalf("got %#v, want base=10080 (from .env)", cfg.Ports)
	}
}

func TestScaffold_GIVEN_composeDefaultOverriddenByEnv_WHEN_scaffolded_THEN_envValueWinsOverComposeDefault(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${DB_PORT:-3306}:3306"
`)
	writeFile(t, filepath.Join(repo, ".env"), "DB_PORT=3307\n")

	cfg := scaffoldAndParse(t, repo)

	p, ok := cfg.Ports["DB_PORT"]
	if !ok || p.Base == nil || *p.Base != 3307 {
		t.Fatalf("got %#v, want base=3307 (env wins over the 3306 compose default)", cfg.Ports)
	}
}

func TestScaffold_GIVEN_composeWithLiteralBinding_WHEN_scaffolded_THEN_noPortsEntryOnlyServiceComment(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  proxy:
    ports:
      - "8443:443"
`)

	cfg := scaffoldAndParse(t, repo)

	if len(cfg.Ports) != 0 {
		t.Fatalf("got ports %#v, want none (a literal binding gets no ports: entry and no .env var)", cfg.Ports)
	}

	data := readGeneratedConfig(t, repo)
	// Discovery went through docker compose config (already merged), so
	// no single source filename can be named for this binding — the
	// comment names the service only.
	wantComment := "# service proxy cannot be replicated: non-parameterized host port 8443"
	if !strings.Contains(data, wantComment) {
		t.Fatalf("expected generated config to contain the service comment, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_serviceWithMultipleLiteralPorts_WHEN_scaffolded_THEN_groupedOnOneCommentLine(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  qdrant:
    ports:
      - "6333:6333"
      - "6334:6334"
`)

	cfg := scaffoldAndParse(t, repo)
	if len(cfg.Ports) != 0 {
		t.Fatalf("got ports %#v, want none", cfg.Ports)
	}

	data := readGeneratedConfig(t, repo)
	wantComment := "# service qdrant cannot be replicated: non-parameterized host port 6333, 6334"
	if !strings.Contains(data, wantComment) {
		t.Fatalf("expected both literal ports grouped on one comment line, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_serviceWithMixOfVarAndLiteralPorts_WHEN_scaffolded_THEN_varEntryAndLiteralCommentBothEmitted(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  qdrant:
    ports:
      - "${QDRANT_HTTP_PORT:-6333}:6333"
      - "6334:6334"
`)

	cfg := scaffoldAndParse(t, repo)

	p, ok := cfg.Ports["QDRANT_HTTP_PORT"]
	if !ok || p.Base == nil || *p.Base != 6333 {
		t.Fatalf("got %#v, want QDRANT_HTTP_PORT base=6333", cfg.Ports)
	}
	if _, ok := cfg.Ports["CT_PORT_6334"]; ok {
		t.Fatalf("got %#v, want no entry for the literal 6334 binding", cfg.Ports)
	}

	data := readGeneratedConfig(t, repo)
	wantComment := "# service qdrant cannot be replicated: non-parameterized host port 6334"
	if !strings.Contains(data, wantComment) {
		t.Fatalf("expected generated config to contain the literal-port comment, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_ipPrefixedBinding_WHEN_scaffolded_THEN_ipIgnoredAndVarParsed(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  buggregator:
    ports:
      - "127.0.0.1:${BUGGREGATOR_HTTP_PORT:-8000}:8000"
`)

	cfg := scaffoldAndParse(t, repo)

	p, ok := cfg.Ports["BUGGREGATOR_HTTP_PORT"]
	if !ok || p.Base == nil || *p.Base != 8000 {
		t.Fatalf("got %#v, want BUGGREGATOR_HTTP_PORT base=8000", cfg.Ports)
	}
}

func TestScaffold_GIVEN_protoSuffixBinding_WHEN_scaffolded_THEN_protoStrippedFromContainerPort(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  dns:
    ports:
      - "${DNS_PORT:-5300}:53/udp"
`)

	cfg := scaffoldAndParse(t, repo)

	p, ok := cfg.Ports["DNS_PORT"]
	if !ok || p.Base == nil || *p.Base != 5300 {
		t.Fatalf("got %#v, want DNS_PORT base=5300", cfg.Ports)
	}
}

func TestScaffold_GIVEN_composeLongFormPortSyntax_WHEN_scaffolded_THEN_publishedAndTargetParsed(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - target: 80
        published: "${PROXY_HTTP_PORT:-8080}"
`)

	cfg := scaffoldAndParse(t, repo)

	p, ok := cfg.Ports["PROXY_HTTP_PORT"]
	if !ok || p.Base == nil || *p.Base != 8080 {
		t.Fatalf("got %#v, want base 8080", cfg.Ports)
	}
}

func TestScaffold_GIVEN_composeLongFormLiteralPublished_WHEN_scaffolded_THEN_treatedAsLiteralBinding(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - target: 80
        published: 8080
`)

	cfg := scaffoldAndParse(t, repo)

	if len(cfg.Ports) != 0 {
		t.Fatalf("got ports %#v, want none (long-form literal published is still a literal binding)", cfg.Ports)
	}

	data := readGeneratedConfig(t, repo)
	wantComment := "# service web cannot be replicated: non-parameterized host port 8080"
	if !strings.Contains(data, wantComment) {
		t.Fatalf("expected generated config to contain the service comment, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_duplicatePortVarAcrossServices_WHEN_scaffolded_THEN_deduped(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${DB_PORT:-3306}:3306"
  web2:
    ports:
      - "${DB_PORT:-3306}:3306"
`)

	cfg := scaffoldAndParse(t, repo)
	if len(cfg.Ports) != 1 {
		t.Fatalf("got %d ports, want 1 (deduped): %#v", len(cfg.Ports), cfg.Ports)
	}
}

func TestScaffold_GIVEN_dockerComposeYAMLBaseWithOverride_WHEN_scaffolded_THEN_bothFilesScanned(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  a:
    ports:
      - "${A_PORT:-1000}:1"
`)
	writeFile(t, filepath.Join(repo, "docker-compose.override.yml"), `
services:
  b:
    ports:
      - "${B_PORT:-2000}:2"
`)

	cfg := scaffoldAndParse(t, repo)
	if _, ok := cfg.Ports["A_PORT"]; !ok {
		t.Fatalf("expected A_PORT from docker-compose.yml, got %#v", cfg.Ports)
	}
	if _, ok := cfg.Ports["B_PORT"]; !ok {
		t.Fatalf("expected B_PORT from the matching docker-compose.override.yml, got %#v", cfg.Ports)
	}
}

func TestScaffold_GIVEN_composeFileFromEnvCOMPOSE_FILE_WHEN_scaffolded_THEN_thatListIsUsed(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  a:
    ports:
      - "${A_PORT:-1000}:1"
`)
	writeFile(t, filepath.Join(repo, "extra.yml"), `
services:
  b:
    ports:
      - "${B_PORT:-2000}:2"
`)
	// COMPOSE_FILE deliberately omits docker-compose.yml, so only extra.yml
	// (and its var) should be scanned.
	writeFile(t, filepath.Join(repo, ".env"), "COMPOSE_FILE=extra.yml\n")

	cfg := scaffoldAndParse(t, repo)
	if _, ok := cfg.Ports["A_PORT"]; ok {
		t.Fatalf("did not expect A_PORT (docker-compose.yml not in COMPOSE_FILE), got %#v", cfg.Ports)
	}
	if _, ok := cfg.Ports["B_PORT"]; !ok {
		t.Fatalf("expected B_PORT from extra.yml (named by COMPOSE_FILE), got %#v", cfg.Ports)
	}
}

func TestScaffold_GIVEN_collidingPortSeries_WHEN_scaffolded_THEN_laterVarStepBumpedToAvoidOverlap(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${A_PORT:-8000}:1"
      - "${B_PORT:-8010}:2"
`)

	cfg := scaffoldAndParse(t, repo)

	a, b := cfg.Ports["A_PORT"], cfg.Ports["B_PORT"]
	if a.Step != 100 || b.Step != 100 {
		t.Fatalf("expected both vars' steps bumped to 100 to resolve the 8000/8010 collision, got A=%#v B=%#v", a, b)
	}
	if *a.Base != 8000 || *b.Base != 8010 {
		t.Fatalf("expected bases to stay pinned to their resolved values, got A=%#v B=%#v", a, b)
	}
}

func TestScaffold_GIVEN_varSeriesHitsLiteralPort_WHEN_scaffolded_THEN_stepBumpedToAvoidLiteral(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  qdrant:
    ports:
      - "6333:6333"
      - "6334:6334"
  web:
    ports:
      - "${WEB_PORT:-6323}:1"
`)

	cfg := scaffoldAndParse(t, repo)

	web, ok := cfg.Ports["WEB_PORT"]
	if !ok {
		t.Fatalf("expected WEB_PORT in ports, got %#v", cfg.Ports)
	}
	// 6323 + 1*10 == 6333, the literal qdrant port: WEB_PORT's step must
	// be bumped away from it, same as it would for another var's series.
	if web.Step != 100 {
		t.Fatalf("expected WEB_PORT's step bumped to 100 to avoid literal port 6333, got %#v", web)
	}
	if web.Base == nil || *web.Base != 6323 {
		t.Fatalf("expected base to stay pinned to 6323, got %#v", web)
	}
}

func TestScaffold_GIVEN_smallGitignoredFile_WHEN_scaffolded_THEN_listedInCommentedCopyBucket(t *testing.T) {
	repo := newFixtureRepo(t)
	// conf/ needs a tracked sibling so the whole dir isn't itself collapsed
	// to "conf/" by git's traditional --ignored mode — this test is about
	// an individual ignored file surfacing on its own, distinct from the
	// gitignored-directory-collapsing behaviour covered separately below.
	writeFile(t, filepath.Join(repo, "conf/README.md"), "tracked\n")
	runGit(t, repo, "add", "conf/README.md")
	runGit(t, repo, "commit", "-m", "add tracked conf file")
	writeFile(t, filepath.Join(repo, ".gitignore"), "conf/config.local.php\nvendor/\n")
	writeFile(t, filepath.Join(repo, "conf/config.local.php"), "<?php\n")
	writeFile(t, filepath.Join(repo, "vendor/pkg/file.php"), "<?php\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "#   - conf/config.local.php") {
		t.Fatalf("expected conf/config.local.php in the commented copy bucket, got:\n%s", data)
	}
	if strings.Contains(data, "vendor/pkg") {
		t.Fatalf("did not expect vendor/ contents in the generic copy bucket, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_smallGitignoredFile_WHEN_scaffolded_THEN_copyBucketCarriesDescription(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "conf/config.local.php\n")
	writeFile(t, filepath.Join(repo, "conf/config.local.php"), "<?php\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	wantDescription := "# copy: gitignored files present in the main checkout"
	if !strings.Contains(data, wantDescription) {
		t.Fatalf("expected copy bucket description, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_repo_WHEN_scaffolded_THEN_filesKeyIsActiveEmptyMap(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "conf/local.php\n")
	writeFile(t, filepath.Join(repo, "conf/local.php"), "<?php\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	cfg := scaffoldAndParse(t, repo)
	if len(cfg.Files.Copy) != 0 || len(cfg.Files.Hardlink) != 0 {
		t.Fatalf("expected files: {} (no active entries), got %#v", cfg.Files)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "files: {}\n") {
		t.Fatalf("expected literal 'files: {}' as the active value, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_gitignoredDirWithMultipleFiles_WHEN_scaffolded_THEN_listedOnceAsDirNotOnePerFile(t *testing.T) {
	repo := newFixtureRepo(t)
	// data/ needs a tracked sibling so only data/docker/ (not the whole
	// data/ dir) is the fully ignored, collapsible unit.
	writeFile(t, filepath.Join(repo, "data/README.md"), "tracked\n")
	runGit(t, repo, "add", "data/README.md")
	runGit(t, repo, "commit", "-m", "add tracked data file")
	writeFile(t, filepath.Join(repo, ".gitignore"), "data/docker/\n")
	writeFile(t, filepath.Join(repo, "data/docker/a.txt"), "a\n")
	writeFile(t, filepath.Join(repo, "data/docker/b.txt"), "b\n")
	writeFile(t, filepath.Join(repo, "data/docker/c.txt"), "c\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "#   - data/docker/") {
		t.Fatalf("expected data/docker/ listed once as a directory in the copy bucket, got:\n%s", data)
	}
	for _, file := range []string{"data/docker/a.txt", "data/docker/b.txt", "data/docker/c.txt"} {
		if strings.Contains(data, file) {
			t.Fatalf("did not expect individual file %q listed once its parent dir is fully ignored, got:\n%s", file, data)
		}
	}
}

func TestScaffold_GIVEN_gitignoredDirWithForceAddedTrackedFile_WHEN_scaffolded_THEN_collapsedToShallowestIgnoredAncestor(t *testing.T) {
	repo := newFixtureRepo(t)
	// data/ itself is gitignored, but data/.gitkeep is force-added so the
	// dir isn't fully untracked-or-ignored — git's own --ignored collapsing
	// would otherwise still surface data/docker/ and data/config.bin
	// individually. Both must collapse to data/, the shallowest ancestor
	// that actually matches the gitignore rule.
	writeFile(t, filepath.Join(repo, ".gitignore"), "data/\n")
	writeFile(t, filepath.Join(repo, "data/.gitkeep"), "")
	writeFile(t, filepath.Join(repo, "data/docker/x.bin"), "x\n")
	writeFile(t, filepath.Join(repo, "data/config.bin"), "y\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "add", "-f", "data/.gitkeep")
	runGit(t, repo, "commit", "-m", "add gitignore and force-added tracked file")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "#   - data/\n") {
		t.Fatalf("expected data/ collapsed to its shallowest ignored ancestor, got:\n%s", data)
	}
	if strings.Contains(data, "data/docker") || strings.Contains(data, "data/config.bin") {
		t.Fatalf("did not expect data/docker or data/config.bin listed once data/ itself is ignored, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_topLevelGitignoredFile_WHEN_scaffolded_THEN_stillListedAsFile(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "secrets.local.env\n")
	writeFile(t, filepath.Join(repo, "secrets.local.env"), "TOKEN=x\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "#   - secrets.local.env") {
		t.Fatalf("expected top-level gitignored file listed as a file in the copy bucket, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_vendorTopLevelIgnoredDir_WHEN_scaffolded_THEN_suggestedAsHardlinkNotRepeatedInCopy(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "vendor/\n")
	writeFile(t, filepath.Join(repo, "vendor/pkg/file.php"), "<?php\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "# hardlink:") || !strings.Contains(data, "#   - vendor") {
		t.Fatalf("expected commented-out hardlink suggestion for vendor/, got:\n%s", data)
	}

	copyBucketIdx := strings.Index(data, "# copy:")
	if copyBucketIdx >= 0 && strings.Contains(data[copyBucketIdx:], "vendor") {
		t.Fatalf("did not expect vendor to also appear in the generic copy bucket, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_ideaTopLevelIgnoredDir_WHEN_scaffolded_THEN_suggestedUnderOwnIDEKey(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".idea/\nnotes.local.md\n")
	writeFile(t, filepath.Join(repo, ".idea/workspace.xml"), "<xml/>")
	writeFile(t, filepath.Join(repo, "notes.local.md"), "wip\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if strings.Contains(data, "# copy: IDE config") {
		t.Fatalf("did not expect the old 'copy: IDE config' heading, got:\n%s", data)
	}
	wantDescription := "# ide: IDE config dirs, copied verbatim (per-worktree patching → post_create hook)"
	if !strings.Contains(data, wantDescription) {
		t.Fatalf("expected ide bucket description, got:\n%s", data)
	}
	if !strings.Contains(data, "# ide:\n") {
		t.Fatalf("expected the ide bucket to render under its own '# ide:' key, got:\n%s", data)
	}
	if !strings.Contains(data, "#   - .idea") {
		t.Fatalf("expected .idea listed under the ide bucket, got:\n%s", data)
	}

	ideIdx := strings.Index(data, "# ide:\n")
	copyIdx := strings.Index(data, "# copy:")
	if ideIdx < 0 || copyIdx < 0 || !strings.Contains(data[:copyIdx], ".idea") {
		t.Fatalf("expected .idea to be listed before the copy bucket starts, got:\n%s", data)
	}
	if strings.Contains(data[copyIdx:], ".idea") {
		t.Fatalf("did not expect .idea repeated under the copy bucket, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_untrackedNonIgnoredFile_WHEN_scaffolded_THEN_alsoBucketedAsCopy(t *testing.T) {
	repo := newFixtureRepo(t)
	// Untracked but NOT covered by any .gitignore rule -> reported as "??"
	// by git status, not "!!"; the scan must include both.
	writeFile(t, filepath.Join(repo, "notes.local.md"), "wip notes\n")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "#   - notes.local.md") {
		t.Fatalf("expected untracked notes.local.md in the copy bucket, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_manyGitignoredFiles_WHEN_scaffolded_THEN_everyEntryListedUncapped(t *testing.T) {
	repo := newFixtureRepo(t)
	var gitignore strings.Builder
	for i := 0; i < 30; i++ {
		gitignore.WriteString(fmt.Sprintf("file%02d.txt\n", i))
		writeFile(t, filepath.Join(repo, fmt.Sprintf("file%02d.txt", i)), "x")
	}
	writeFile(t, filepath.Join(repo, ".gitignore"), gitignore.String())
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if got := strings.Count(data, "#   - file"); got != 30 {
		t.Fatalf("got %d commented copy entries, want all 30 listed uncapped:\n%s", got, data)
	}
	if strings.Contains(data, "more") {
		t.Fatalf("expected no truncation note, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_gitignoredCloneTreeDir_WHEN_scaffolded_THEN_excluded(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "ignored-stuff.txt\n")
	writeFile(t, filepath.Join(repo, "ignored-stuff.txt"), "x")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "add gitignore")

	// .clone-tree/ is created by Scaffold itself and must never be
	// suggested back to itself.
	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if strings.Contains(data, ".clone-tree/config.yaml") {
		t.Fatalf("did not expect .clone-tree/ contents suggested back to itself, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_repo_WHEN_scaffolded_THEN_headerCommentEmitted(t *testing.T) {
	repo := newFixtureRepo(t)

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.HasPrefix(data, "# generated by ct") {
		t.Fatalf("expected header comment, got:\n%s", data)
	}
	wantWorktreesDir := fmt.Sprintf("worktrees_dir: ../%s-worktrees\n", filepath.Base(repo))
	if !strings.Contains(data, wantWorktreesDir) {
		t.Fatalf("got worktrees_dir line, want %q, got:\n%s", wantWorktreesDir, data)
	}
	if !strings.Contains(data, `# dns_pattern: per-worktree hostname, {name} = "{name}.myapp.localhost". Empty → localhost`) {
		t.Fatalf("expected dns_pattern example comment, got:\n%s", data)
	}
	if !strings.Contains(data, `#   APP_NAME: "myapp-{name}"`) {
		t.Fatalf("expected env example comment, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_repo_WHEN_scaffolded_THEN_defaultsMatchSpec(t *testing.T) {
	repo := newFixtureRepo(t)

	cfg := scaffoldAndParse(t, repo)
	if cfg.DNSPattern != "" {
		t.Fatalf("got dns_pattern %q, want empty", cfg.DNSPattern)
	}
	if cfg.MaxSlots != 9 {
		t.Fatalf("got max_slots %d, want 9", cfg.MaxSlots)
	}
	want := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-worktrees")
	if cfg.WorktreesDir != want {
		t.Fatalf("got worktrees_dir %q, want %q", cfg.WorktreesDir, want)
	}
}

func TestScaffold_GIVEN_unresolvedPortVar_WHEN_scaffoldedThenLoaded_THEN_loadErrorsNamingTheVar(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${PROXY_HTTP_PORT}:80"
`)

	path, err := config.Scaffold(repo)
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	_, err = config.LoadExisting(repo, path)
	if err == nil {
		t.Fatalf("expected LoadExisting to error on an unresolved var (base: null written, but config now invalid)")
	}
	if !strings.Contains(err.Error(), "set base for PROXY_HTTP_PORT in .clone-tree/config.yaml") {
		t.Fatalf("got %q, want it to name the var and the file to fix", err.Error())
	}

	// The file is still on disk for review even though it's invalid.
	if _, statErr := os.Stat(filepath.Join(repo, ".clone-tree", "config.yaml")); statErr != nil {
		t.Fatalf("expected the generated config to still be on disk for review: %v", statErr)
	}
}

func TestScaffold_GIVEN_httpsContainerPort_WHEN_scaffolded_THEN_urlSuggestionUsesHttpsScheme(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  proxy:
    ports:
      - "${PROXY_HTTPS_PORT:-10443}:443"
`)

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	want := `#   proxy: "https://localhost:{PROXY_HTTPS_PORT}/"`
	if !strings.Contains(data, want) {
		t.Fatalf("expected %q in generated config, got:\n%s", want, data)
	}
}

func TestScaffold_GIVEN_plainContainerPort_WHEN_scaffolded_THEN_urlSuggestionUsesHttpScheme(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  web:
    ports:
      - "${PROXY_HTTP_PORT:-8080}:80"
`)

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	want := `#   web: "http://localhost:{PROXY_HTTP_PORT}/"`
	if !strings.Contains(data, want) {
		t.Fatalf("expected %q in generated config, got:\n%s", want, data)
	}
}

func TestScaffold_GIVEN_serviceExposingTwoPortVars_WHEN_scaffolded_THEN_secondUrlLabelGetsDashTwoSuffix(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  proxy:
    ports:
      - "${PROXY_HTTP_PORT:-8080}:80"
      - "${PROXY_HTTPS_PORT:-8443}:443"
`)

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	for _, want := range []string{
		`#   proxy: "http://localhost:{PROXY_HTTP_PORT}/"`,
		`#   proxy-2: "https://localhost:{PROXY_HTTPS_PORT}/"`,
	} {
		if !strings.Contains(data, want) {
			t.Fatalf("expected %q in generated config, got:\n%s", want, data)
		}
	}
}

func TestScaffold_GIVEN_literalPortBinding_WHEN_scaffolded_THEN_noUrlSuggestion(t *testing.T) {
	repo := newFixtureRepo(t)
	writeFile(t, filepath.Join(repo, "docker-compose.yml"), `
services:
  qdrant:
    ports:
      - "6333:6333"
`)

	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	data := readGeneratedConfig(t, repo)
	if strings.Contains(data, "qdrant:") && strings.Contains(data, "{6333}") {
		t.Fatalf("did not expect a url suggestion for a literal (non-parameterized) binding, got:\n%s", data)
	}
	if !strings.Contains(data, "urls: {}\n") {
		t.Fatalf("expected the live 'urls: {}' value even with no suggestions, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_noComposeFile_WHEN_scaffolded_THEN_urlsKeyIsActiveEmptyMap(t *testing.T) {
	repo := newFixtureRepo(t)

	cfg := scaffoldAndParse(t, repo)
	if len(cfg.Urls) != 0 {
		t.Fatalf("expected urls: {} (no active entries), got %#v", cfg.Urls)
	}

	data := readGeneratedConfig(t, repo)
	if !strings.Contains(data, "urls: {}\n") {
		t.Fatalf("expected literal 'urls: {}' as the active value, got:\n%s", data)
	}
}

func TestScaffoldOrError_GIVEN_repo_WHEN_called_THEN_scaffoldsAndReturnsScaffoldedError(t *testing.T) {
	repo := newFixtureRepo(t)

	err := config.ScaffoldOrError(repo)

	var scaffolded *config.ScaffoldedError
	if !errors.As(err, &scaffolded) {
		t.Fatalf("got err %v, want *config.ScaffoldedError", err)
	}
	wantPath := filepath.Join(repo, ".clone-tree", "config.yaml")
	if scaffolded.Path != wantPath {
		t.Fatalf("got path %q, want %q", scaffolded.Path, wantPath)
	}
	msg := err.Error()
	for _, want := range []string{wantPath, "set me", "SUGGESTION", "Uncomment", "collision", "cannot be replicated", "localhost", "APP_NAME"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("got error %q, want it to contain %q", msg, want)
		}
	}
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Fatalf("expected scaffold to write config.yaml: %v", statErr)
	}
}

func TestScaffold_GIVEN_mainBranch_WHEN_scaffolded_THEN_baseBranchIsMain(t *testing.T) {
	repo := newFixtureRepo(t)

	data := readGeneratedConfigAfterScaffold(t, repo)
	if !strings.Contains(data, "base_branch: main\n") {
		t.Fatalf("expected base_branch: main, got:\n%s", data)
	}
}

func TestScaffold_GIVEN_masterOnlyBranch_WHEN_scaffolded_THEN_baseBranchIsMaster(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "--initial-branch=master")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repo, "README.md"), "fixture\n")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial commit")

	data := readGeneratedConfigAfterScaffold(t, repo)
	if !strings.Contains(data, "base_branch: master\n") {
		t.Fatalf("expected base_branch: master, got:\n%s", data)
	}
}

func TestDetectBaseBranch_GIVEN_masterOnlyRepo_WHEN_detected_THEN_masterReturnedTrue(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "--initial-branch=master")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repo, "README.md"), "fixture\n")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial commit")

	got, ok := config.DetectBaseBranch(repo)
	if !ok || got != "master" {
		t.Fatalf("got (%q, %v), want (\"master\", true)", got, ok)
	}
}

func TestDetectBaseBranch_GIVEN_neitherMainNorMaster_WHEN_detected_THEN_falseReturned(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "--initial-branch=trunk")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repo, "README.md"), "fixture\n")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial commit")

	got, ok := config.DetectBaseBranch(repo)
	if ok {
		t.Fatalf("got (%q, %v), want (\"\", false)", got, ok)
	}
}

// readGeneratedConfigAfterScaffold scaffolds repo and returns the raw
// generated config.yaml text (unparsed — used by tests that only assert on
// the rendered YAML, not the parsed Config).
func readGeneratedConfigAfterScaffold(t *testing.T, repo string) string {
	t.Helper()
	if _, err := config.Scaffold(repo); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	return readGeneratedConfig(t, repo)
}
