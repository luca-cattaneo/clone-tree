package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newCreateFixtureRepo creates a git repository named "repo" inside a fresh
// projects directory (projectsDir/repo), with a single initial commit on
// main. It returns the projects dir and the repo dir.
func newCreateFixtureRepo(t *testing.T) (projectsDir, repoDir string) {
	t.Helper()

	projectsDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	repoDir = filepath.Join(projectsDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runGit(t, repoDir, "init", "--initial-branch=main")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test")

	readme := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readme, []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "initial commit")

	return projectsDir, repoDir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// chdir switches the process cwd to dir for the duration of the test
// (createCmd resolves its repo root from os.Getwd via cwd()), restoring
// the original cwd on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestCreate_GIVEN_cloneCowSucceedsThenLaterStepFails_WHEN_created_THEN_rollbackRemovesCloneCowDst(t *testing.T) {
	projectsDir, repoDir := newCreateFixtureRepo(t)

	dataSrc := filepath.Join(projectsDir, "data")
	if err := os.MkdirAll(dataSrc, 0o755); err != nil {
		t.Fatalf("MkdirAll dataSrc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataSrc, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	worktreesDir := filepath.Join(projectsDir, "repo-worktrees")
	// A subdirectory of worktreesDir stripped of write permission: forces
	// the symlink creation itself to fail (EACCES) after clone_cow has
	// already succeeded, without blocking gitwt.Create's own writes into
	// worktreesDir/feature (a sibling, unaffected, entry).
	blockedDir := filepath.Join(worktreesDir, "blocked")
	if err := os.MkdirAll(blockedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll blockedDir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectsDir, "blocked", "sibling"), 0o755); err != nil {
		t.Fatalf("MkdirAll sibling source: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o555); err != nil {
		t.Fatalf("Chmod blockedDir: %v", err)
	}

	configYAML := "version: 1\n" +
		"worktrees_dir: ../repo-worktrees\n" +
		"dns_pattern: \"\"\n" +
		"max_slots: 9\n" +
		"ports: {}\n" +
		"env: {}\n" +
		"files:\n" +
		"  clone_cow:\n" +
		"    - {src: \"{projects_dir}/data\", dst: \"{projects_dir}/data_{name}\"}\n" +
		"  symlink_siblings: [blocked/sibling]\n" +
		"hooks: {}\n"
	if err := os.MkdirAll(filepath.Join(repoDir, ".clone-tree"), 0o755); err != nil {
		t.Fatalf("MkdirAll .clone-tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".clone-tree", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	chdir(t, repoDir)
	configPath = ""
	createBranch = ""

	err := createCmd.RunE(createCmd, []string{"feature"})
	if err == nil {
		t.Fatalf("expected create to fail when the sibling symlink cannot be written")
	}

	cloneCoWDst := filepath.Join(projectsDir, "data_feature")
	if _, statErr := os.Stat(cloneCoWDst); !os.IsNotExist(statErr) {
		t.Fatalf("expected rollback to remove %s, stat err: %v", cloneCoWDst, statErr)
	}

	if _, statErr := os.Stat(filepath.Join(worktreesDir, "feature")); !os.IsNotExist(statErr) {
		t.Fatalf("expected the worktree itself to be rolled back too, stat err: %v", statErr)
	}
}

func TestCreate_GIVEN_candidateSlotPortAlreadyBusy_WHEN_created_THEN_errorsAndNothingTouched(t *testing.T) {
	_, repoDir := newCreateFixtureRepo(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	busyPort := ln.Addr().(*net.TCPAddr).Port

	// The candidate slot for a fresh repo is slot 1, so base+1*step must
	// equal the busy listener's port.
	configYAML := "version: 1\n" +
		"worktrees_dir: ../repo-worktrees\n" +
		"dns_pattern: \"\"\n" +
		"max_slots: 9\n" +
		fmt.Sprintf("ports:\n  DB_PORT: {base: %d, step: 10}\n", busyPort-10) +
		"env: {}\n" +
		"files: {}\n" +
		"hooks: {}\n"
	if err := os.MkdirAll(filepath.Join(repoDir, ".clone-tree"), 0o755); err != nil {
		t.Fatalf("MkdirAll .clone-tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".clone-tree", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	chdir(t, repoDir)
	configPath = ""
	createBranch = ""

	err = createCmd.RunE(createCmd, []string{"feature"})
	if err == nil {
		t.Fatalf("expected create to fail: the configured DB_PORT slot-1 value is the busy listener's port")
	}

	if _, statErr := os.Stat(filepath.Join(filepath.Dir(repoDir), "repo-worktrees", "feature")); !os.IsNotExist(statErr) {
		t.Fatalf("expected nothing to be touched (no worktree created), stat err: %v", statErr)
	}
}

func TestCreate_GIVEN_filesConfigured_WHEN_created_THEN_copyHardlinkCloneCowAndSymlinkApplied(t *testing.T) {
	projectsDir, repoDir := newCreateFixtureRepo(t)

	// files.copy source, gitignored in a real repo but plain here.
	if err := os.WriteFile(filepath.Join(repoDir, "conf.local"), []byte("local-conf"), 0o644); err != nil {
		t.Fatalf("WriteFile conf.local: %v", err)
	}
	// files.ide source.
	ideaSrc := filepath.Join(repoDir, ".idea")
	if err := os.MkdirAll(ideaSrc, 0o755); err != nil {
		t.Fatalf("MkdirAll .idea: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ideaSrc, "workspace.xml"), []byte("<xml/>"), 0o644); err != nil {
		t.Fatalf("WriteFile workspace.xml: %v", err)
	}
	// files.hardlink source.
	vendorSrc := filepath.Join(repoDir, "vendor")
	if err := os.MkdirAll(vendorSrc, 0o755); err != nil {
		t.Fatalf("MkdirAll vendor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vendorSrc, "lib.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatalf("WriteFile lib.php: %v", err)
	}
	// clone_cow source.
	dataSrc := filepath.Join(projectsDir, "data")
	if err := os.MkdirAll(dataSrc, 0o755); err != nil {
		t.Fatalf("MkdirAll dataSrc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataSrc, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("WriteFile seed.txt: %v", err)
	}
	// symlink_siblings source: must exist under projectsDir for the sibling
	// link to be created.
	if err := os.MkdirAll(filepath.Join(projectsDir, "sibling"), 0o755); err != nil {
		t.Fatalf("MkdirAll sibling source: %v", err)
	}

	configYAML := "version: 1\n" +
		"worktrees_dir: ../repo-worktrees\n" +
		"dns_pattern: \"\"\n" +
		"max_slots: 9\n" +
		"ports: {}\n" +
		"env: {}\n" +
		"files:\n" +
		"  ide: [.idea]\n" +
		"  copy: [conf.local]\n" +
		"  hardlink: [vendor]\n" +
		"  clone_cow:\n" +
		"    - {src: \"{projects_dir}/data\", dst: \"{projects_dir}/data_{name}\"}\n" +
		"  symlink_siblings: [sibling]\n" +
		"hooks: {}\n"
	if err := os.MkdirAll(filepath.Join(repoDir, ".clone-tree"), 0o755); err != nil {
		t.Fatalf("MkdirAll .clone-tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".clone-tree", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	chdir(t, repoDir)
	configPath = ""
	createBranch = ""

	if err := createCmd.RunE(createCmd, []string{"feature"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	worktreesDir := filepath.Join(projectsDir, "repo-worktrees")
	target := filepath.Join(worktreesDir, "feature")

	got, err := os.ReadFile(filepath.Join(target, "conf.local"))
	if err != nil || string(got) != "local-conf" {
		t.Fatalf("copy: got %q, err %v", got, err)
	}

	gotIDE, err := os.ReadFile(filepath.Join(target, ".idea", "workspace.xml"))
	if err != nil || string(gotIDE) != "<xml/>" {
		t.Fatalf("ide: got %q, err %v", gotIDE, err)
	}

	srcInfo, err := os.Stat(filepath.Join(vendorSrc, "lib.php"))
	if err != nil {
		t.Fatalf("Stat vendor src: %v", err)
	}
	dstInfo, err := os.Stat(filepath.Join(target, "vendor", "lib.php"))
	if err != nil {
		t.Fatalf("Stat vendor dst: %v", err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Fatalf("expected hardlinked vendor file to share an inode")
	}

	got, err = os.ReadFile(filepath.Join(projectsDir, "data_feature", "seed.txt"))
	if err != nil || string(got) != "seed" {
		t.Fatalf("clone_cow: got %q, err %v", got, err)
	}

	link, err := os.Readlink(filepath.Join(worktreesDir, "sibling"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if want := "../sibling"; link != want {
		t.Fatalf("symlink_siblings: got %q, want %q", link, want)
	}
}
