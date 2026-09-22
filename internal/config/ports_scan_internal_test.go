package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBindingString_GIVEN_bindingGrammarTable_WHEN_parsed_THEN_piecesExtracted(t *testing.T) {
	tests := []struct {
		name          string
		binding       string
		wantVar       string
		wantLiteral   int
		wantHasDflt   bool
		wantContainer int
	}{
		{"colon-dash default", "${DB_PORT:-3306}:3306", "DB_PORT", 3306, true, 3306},
		{"dash default", "${DB_PORT-3306}:3306", "DB_PORT", 3306, true, 3306},
		{"bare var", "${DB_PORT}:3306", "DB_PORT", 0, false, 3306},
		{"literal", "8443:443", "", 8443, false, 443},
		{"ip prefix", "127.0.0.1:${APP_HTTP_PORT}:8000", "APP_HTTP_PORT", 0, false, 8000},
		{"ip prefix with default", "127.0.0.1:${MINIO_PORT:-1081}:81", "MINIO_PORT", 1081, true, 81},
		{"templated ip prefix (real-world shape)", "${BIND_IP:-0.0.0.0}:${DB_PORT-3306}:3306", "DB_PORT", 3306, true, 3306},
		{"templated ip prefix with colon-dash default", "${BIND_IP:-0.0.0.0}:${PROXY_HTTP_PORT:-80}:80", "PROXY_HTTP_PORT", 80, true, 80},
		{"proto suffix", "${DB_PORT:-3306}:3306/udp", "DB_PORT", 3306, true, 3306},
		{"ip and literal", "127.0.0.1:8080:80", "", 8080, false, 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb, ok := parseBindingString(tt.binding, "docker-compose.yml")
			if !ok {
				t.Fatalf("parseBindingString(%q): expected ok", tt.binding)
			}
			if rb.varName != tt.wantVar || rb.literal != tt.wantLiteral || rb.hasDefault != tt.wantHasDflt || rb.containerPort != tt.wantContainer {
				t.Fatalf("got %#v, want varName=%q literal=%d hasDefault=%v containerPort=%d",
					rb, tt.wantVar, tt.wantLiteral, tt.wantHasDflt, tt.wantContainer)
			}
		})
	}
}

func TestParseBindingString_GIVEN_longSyntaxReassembled_WHEN_parsed_THEN_sameAsShortForm(t *testing.T) {
	// extractBinding reassembles long-form {target, published} into
	// "<published>:<target>" before parseBindingString ever sees it; this
	// exercises that reassembled string directly.
	rb, ok := parseBindingString("${PROXY_HTTP_PORT:-8080}:80", "docker-compose.yml")
	if !ok {
		t.Fatalf("expected ok")
	}
	if rb.varName != "PROXY_HTTP_PORT" || rb.literal != 8080 {
		t.Fatalf("got %#v", rb)
	}
}

func TestBaseStep_GIVEN_originalTable_WHEN_computed_THEN_ruleApplied(t *testing.T) {
	tests := []struct {
		original, wantBase, wantStep int
	}{
		{3306, 3306, 10},
		{1024, 1024, 10},
		{8080, 8080, 10},
		{443, 10443, 10},
		{80, 10080, 10},
		{1023, 11023, 10},
	}
	for _, tt := range tests {
		base, step := baseStep(tt.original)
		if base != tt.wantBase || step != tt.wantStep {
			t.Fatalf("baseStep(%d) = (%d, %d), want (%d, %d)", tt.original, base, step, tt.wantBase, tt.wantStep)
		}
	}
}

func TestResolveOne_GIVEN_bareVarMissingFromEnv_WHEN_resolved_THEN_nilBaseWithSetMeComment(t *testing.T) {
	rb := rawBinding{varName: "PROXY_HTTP_PORT", containerPort: 80}
	p := resolveOne(rb, map[string]string{})

	if p.Base != nil {
		t.Fatalf("got base %v, want nil", *p.Base)
	}
	want := "# set me: ${PROXY_HTTP_PORT} has no value in .env"
	if len(p.Comment) != 1 || p.Comment[0] != want {
		t.Fatalf("got comment %#v, want %q", p.Comment, want)
	}
}

func TestResolveOne_GIVEN_bareVarPresentInEnv_WHEN_resolved_THEN_envValueUsedAsBase(t *testing.T) {
	rb := rawBinding{varName: "PROXY_HTTP_PORT", containerPort: 80}
	p := resolveOne(rb, map[string]string{"PROXY_HTTP_PORT": "10080"})

	if p.Base == nil || *p.Base != 10080 {
		t.Fatalf("got base %v, want 10080", p.Base)
	}
	if len(p.Comment) != 0 {
		t.Fatalf("got comment %#v, want none", p.Comment)
	}
}

func TestResolveOne_GIVEN_defaultVarOverriddenInEnv_WHEN_resolved_THEN_envValueWinsOverDefault(t *testing.T) {
	rb := rawBinding{varName: "DB_PORT", literal: 3306, hasDefault: true, containerPort: 3306}
	p := resolveOne(rb, map[string]string{"DB_PORT": "3307"})

	if p.Base == nil || *p.Base != 3307 {
		t.Fatalf("got base %v, want 3307 (env override, not the compose default)", p.Base)
	}
}

func TestResolveCollisions_GIVEN_seriesOverlapAtSlotOne_WHEN_resolved_THEN_offendingVarsStepBumped(t *testing.T) {
	// A's slot-1 value (8000+10=8010) equals B's base: A is the "mover"
	// here (B's base is pinned), so A's step gets bumped. That in turn
	// makes A's new slot-1 (8000+100=8100) collide with B's original
	// slot-9 (8010+9*10=8100), so B's step is bumped too on the next pass
	// — both settle at step 100, clean of each other.
	a, b := 8000, 8010
	ports := []scaffoldPort{
		{Name: "A_PORT", Base: &a, Step: 10},
		{Name: "B_PORT", Base: &b, Step: 10},
	}

	resolveCollisions(ports, 9, nil)

	if ports[0].Step != 100 {
		t.Fatalf("expected A_PORT's step bumped to 100, got %d", ports[0].Step)
	}
	if ports[1].Step != 100 {
		t.Fatalf("expected B_PORT's step bumped to 100, got %d", ports[1].Step)
	}
	if *ports[0].Base != 8000 || *ports[1].Base != 8010 {
		t.Fatalf("expected bases to stay pinned, got A=%d B=%d", *ports[0].Base, *ports[1].Base)
	}
	if len(ports[0].Comment) != 0 || len(ports[1].Comment) != 0 {
		t.Fatalf("expected a clean resolution with no collision comment, got A=%#v B=%#v", ports[0].Comment, ports[1].Comment)
	}
}

func TestResolveCollisions_GIVEN_stillCollidingAfterMaxBumps_WHEN_resolved_THEN_collisionCommentLeft(t *testing.T) {
	// A delta of exactly 1000 between the two bases re-collides at every
	// power-of-ten step (1, 10, 100, 1000) as long as maxSlots is large
	// enough to reach the matching slot each time — so even after the 3
	// allowed bumps, both vars are still exact multiples of each other's
	// series and the pass has to give up and leave a comment.
	a, b := 100, 1100
	ports := []scaffoldPort{
		{Name: "A_PORT", Base: &a, Step: 1},
		{Name: "B_PORT", Base: &b, Step: 1},
	}

	resolveCollisions(ports, 1000, nil)

	for _, p := range ports {
		found := false
		for _, c := range p.Comment {
			if strings.HasPrefix(c, "# collision:") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a collision comment on %s, got %#v", p.Name, p.Comment)
		}
	}
}

func TestResolveCollisions_GIVEN_seriesHitsLiteralPort_WHEN_resolved_THEN_stepBumped(t *testing.T) {
	base := 6320
	ports := []scaffoldPort{
		{Name: "WEB_PORT", Base: &base, Step: 10},
	}

	// 6320 + 1*10 == 6330: slot 1 lands exactly on the literal port.
	resolveCollisions(ports, 9, []int{6330})

	if ports[0].Step != 100 {
		t.Fatalf("expected WEB_PORT's step bumped to 100 to avoid literal port 6330, got %d", ports[0].Step)
	}
	if *ports[0].Base != 6320 {
		t.Fatalf("expected base to stay pinned, got %d", *ports[0].Base)
	}
	if len(ports[0].Comment) != 0 {
		t.Fatalf("expected a clean resolution with no collision comment, got %#v", ports[0].Comment)
	}
}

func TestResolveCollisions_GIVEN_seriesMissesAllLiteralPorts_WHEN_resolved_THEN_untouched(t *testing.T) {
	base := 6320
	ports := []scaffoldPort{
		{Name: "WEB_PORT", Base: &base, Step: 10},
	}

	// 6320's series (6330, 6340, …, 6410) never lands on 9999.
	resolveCollisions(ports, 9, []int{9999})

	if ports[0].Step != 10 {
		t.Fatalf("expected WEB_PORT's step untouched at 10, got %d", ports[0].Step)
	}
}

func TestLiteralPortComments_GIVEN_multipleLiteralsOneService_WHEN_grouped_THEN_onePortSortedLine(t *testing.T) {
	raws := []rawBinding{
		{literal: 6334, svcName: "qdrant"},
		{literal: 6333, svcName: "qdrant"},
	}

	got := literalPortComments(raws)

	want := []string{"# service qdrant cannot be replicated: non-parameterized host port 6333, 6334"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestLiteralPortComments_GIVEN_knownSourceFile_WHEN_grouped_THEN_filenameAppended(t *testing.T) {
	raws := []rawBinding{
		{literal: 9000, svcName: "buggregator", sourceFile: "docker-compose.yml"},
	}

	got := literalPortComments(raws)

	want := "# service buggregator cannot be replicated: non-parameterized host port 9000 (docker-compose.yml)"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %#v, want %q", got, want)
	}
}

func TestLiteralPortComments_GIVEN_unknownSourceFile_WHEN_grouped_THEN_noFilenameSuffix(t *testing.T) {
	raws := []rawBinding{
		{literal: 8443, svcName: "proxy", sourceFile: ""},
	}

	got := literalPortComments(raws)

	want := "# service proxy cannot be replicated: non-parameterized host port 8443"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %#v, want %q", got, want)
	}
}

func TestLiteralPortComments_GIVEN_varBindings_WHEN_grouped_THEN_excluded(t *testing.T) {
	raws := []rawBinding{
		{varName: "DB_PORT", literal: 3306, hasDefault: true, svcName: "db"},
	}

	got := literalPortComments(raws)

	if len(got) != 0 {
		t.Fatalf("got %#v, want none (only literal bindings are grouped)", got)
	}
}

func TestParseDotEnv_GIVEN_commentsBlanksAndQuotes_WHEN_parsed_THEN_onlyKeyValueLinesKept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# a comment\n\nDB_PORT=3306\nQUOTED=\"hello world\"\nSINGLE='abc'\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := parseDotEnv(path)
	if err != nil {
		t.Fatalf("parseDotEnv: %v", err)
	}
	want := map[string]string{"DB_PORT": "3306", "QUOTED": "hello world", "SINGLE": "abc"}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("got %q=%q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want exactly %#v", got, want)
	}
}

func TestParseDotEnv_GIVEN_missingFile_WHEN_parsed_THEN_emptyMapNotFatal(t *testing.T) {
	got, err := parseDotEnv(filepath.Join(t.TempDir(), "nope.env"))
	if err == nil {
		t.Fatalf("expected an error to be returned so the caller can decide to ignore it")
	}
	if len(got) != 0 {
		t.Fatalf("got %#v, want empty map even on error", got)
	}
}

func TestExtractBindingsFallback_GIVEN_overrideTagOnPorts_WHEN_merged_THEN_laterFileWholesaleReplacesEarlier(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "docker-compose.yml")
	override := filepath.Join(dir, "docker-compose.override.yml")

	writeFileForTest(t, base, `
services:
  buggregator:
    ports:
      - "9000:9000"
  web:
    ports:
      - "${PROXY_HTTP_PORT:-8080}:80"
`)
	writeFileForTest(t, override, `
services:
  buggregator:
    ports: !override
      - "127.0.0.1:${APP_HTTP_PORT}:8000"
`)

	raws := extractBindingsFallback([]string{base, override})

	var gotBuggregator, gotWeb bool
	for _, rb := range raws {
		switch rb.varName {
		case "APP_HTTP_PORT":
			gotBuggregator = true
		case "PROXY_HTTP_PORT":
			gotWeb = true
		}
		if rb.literal == 9000 {
			t.Fatalf("expected the base file's literal 9000:9000 binding to be wholly replaced by !override, got %#v", raws)
		}
	}
	if !gotBuggregator {
		t.Fatalf("expected APP_HTTP_PORT (from the !override file) in %#v", raws)
	}
	if !gotWeb {
		t.Fatalf("expected PROXY_HTTP_PORT (untouched by override, appended) in %#v", raws)
	}
}

func TestExtractBindingsFallback_GIVEN_noOverrideTag_WHEN_merged_THEN_laterFileAppends(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "docker-compose.yml")
	extra := filepath.Join(dir, "docker-compose.extra.yml")

	writeFileForTest(t, base, `
services:
  web:
    ports:
      - "${A_PORT:-1000}:1"
`)
	writeFileForTest(t, extra, `
services:
  web:
    ports:
      - "${B_PORT:-2000}:2"
`)

	raws := extractBindingsFallback([]string{base, extra})
	names := map[string]bool{}
	for _, rb := range raws {
		names[rb.varName] = true
	}
	if !names["A_PORT"] || !names["B_PORT"] {
		t.Fatalf("expected both A_PORT and B_PORT (appended, not replaced), got %#v", raws)
	}
}

func TestExtractBindings_GIVEN_dockerComposeUnavailable_WHEN_extracted_THEN_fallbackPathUsed(t *testing.T) {
	orig := runDockerComposeConfig
	runDockerComposeConfig = func(string, []string) ([]byte, error) {
		return nil, errors.New("docker: command not found")
	}
	defer func() { runDockerComposeConfig = orig }()

	dir := t.TempDir()
	f := filepath.Join(dir, "docker-compose.yml")
	writeFileForTest(t, f, `
services:
  web:
    ports:
      - "${A_PORT:-1000}:1"
`)

	raws, discovery := extractBindings(dir, []string{f})
	if discovery != "fallback yaml merge" {
		t.Fatalf("got discovery %q, want %q", discovery, "fallback yaml merge")
	}
	if len(raws) != 1 || raws[0].varName != "A_PORT" {
		t.Fatalf("got %#v", raws)
	}
}

func TestExtractBindings_GIVEN_dockerComposeAvailable_WHEN_extracted_THEN_primaryPathUsed(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed on this host")
	}

	dir := t.TempDir()
	f := filepath.Join(dir, "docker-compose.yml")
	writeFileForTest(t, f, `
services:
  web:
    image: alpine
    ports:
      - "${A_PORT:-1000}:1"
`)

	raws, discovery := extractBindings(dir, []string{f})
	if discovery != "docker compose config" {
		t.Fatalf("got discovery %q, want %q", discovery, "docker compose config")
	}
	if len(raws) != 1 || raws[0].varName != "A_PORT" {
		t.Fatalf("got %#v", raws)
	}
}

func writeFileForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
