package config

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

var hardlinkDirNames = map[string]bool{
	"vendor": true, "node_modules": true, ".venv": true,
	"venv": true, "target": true, ".gradle": true,
}

var ideDirNames = map[string]bool{
	".idea": true, ".vscode": true, ".fleet": true, ".vs": true,
}

type scaffoldFiles struct {
	hardlink bucket
	ide      bucket
	copy     bucket
}

type bucket struct {
	items []string
	seen  map[string]bool
}

func (b *bucket) add(item string) {
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	if b.seen[item] {
		return
	}
	b.seen[item] = true
	b.items = append(b.items, item)
}

func (b *bucket) sorted() []string {
	sort.Strings(b.items)
	return b.items
}

// git's own "traditional" --ignored collapsing only merges a directory into
// one entry when everything under it is untracked-or-ignored, so a
// gitignored dir that also holds tracked content (e.g. a force-added
// `.gitkeep`) would otherwise still surface as several individual
// files/subdirs even though the dir itself matches a gitignore rule —
// collapseToIgnoredAncestors below corrects for that.
func scanGitignored(root string) (scaffoldFiles, error) {
	cmd := exec.Command("git", "status", "--porcelain=v1", "--ignored", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return scaffoldFiles{}, fmt.Errorf("git status --ignored: %w", err)
	}

	var paths []string
	for _, entry := range strings.Split(string(out), "\x00") {
		if len(entry) < 3 {
			continue
		}
		status := entry[:2]
		if status != "??" && status != "!!" {
			continue
		}
		path := entry[3:]
		if isExcludedScaffoldPath(path) {
			continue
		}
		paths = append(paths, path)
	}

	resolved, err := collapseToIgnoredAncestors(root, paths)
	if err != nil {
		return scaffoldFiles{}, err
	}

	var result scaffoldFiles
	for _, path := range resolved {
		top := strings.SplitN(strings.TrimSuffix(path, "/"), "/", 2)[0]
		switch {
		case hardlinkDirNames[top]:
			result.hardlink.add(top)
		case ideDirNames[top]:
			result.ide.add(top)
		default:
			result.copy.add(path)
		}
	}
	return result, nil
}

func collapseToIgnoredAncestors(root string, paths []string) ([]string, error) {
	dirSet := map[string]bool{}
	for _, path := range paths {
		for _, dir := range ancestorDirs(path) {
			dirSet[dir] = true
		}
	}
	dirs := make([]string, 0, len(dirSet))
	for dir := range dirSet {
		dirs = append(dirs, dir)
	}

	ignoredDirs, err := checkIgnoredDirs(root, dirs)
	if err != nil {
		return nil, err
	}

	resolved := make([]string, len(paths))
	for i, path := range paths {
		resolved[i] = path
		for _, dir := range ancestorDirs(path) {
			if ignoredDirs[dir] {
				resolved[i] = dir
				break
			}
		}
	}
	return resolved, nil
}

// ancestorDirs returns path's proper ancestor directories, shallowest
// first, each rendered with a trailing "/" (e.g. "data/docker/x.bin" ->
// ["data/", "data/docker/"]). A top-level path has no ancestors.
func ancestorDirs(path string) []string {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	dirs := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		dirs = append(dirs, strings.Join(parts[:i], "/")+"/")
	}
	return dirs
}

// --no-index makes the check independent of whether anything under the dir
// is already tracked (e.g. a force-added file). Exit status 1 means none of
// the dirs matched — not an error — any other non-zero status is.
func checkIgnoredDirs(root string, dirs []string) (map[string]bool, error) {
	ignored := map[string]bool{}
	if len(dirs) == 0 {
		return ignored, nil
	}

	cmd := exec.Command("git", "check-ignore", "-z", "--stdin", "--no-index")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(strings.Join(dirs, "\x00") + "\x00")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return ignored, nil
		}
		return nil, fmt.Errorf("git check-ignore: %w", err)
	}

	for _, dir := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if dir != "" {
			ignored[dir] = true
		}
	}
	return ignored, nil
}

func isExcludedScaffoldPath(path string) bool {
	for _, prefix := range []string{".clone-tree/", ".git/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
