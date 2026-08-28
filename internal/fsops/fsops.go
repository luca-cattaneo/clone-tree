// Package fsops materializes the gitignored files a fresh worktree needs
// (config, generated assets, big vendor/datadir trees) that a plain
// `git worktree add` cannot supply, since git only tracks committed
// content. Four strategies, one per `files:` config key: Copy, Hardlink,
// CloneCoW, SymlinkSiblings.
package fsops

import (
	"fmt"
	"os"
)

// warnMissing reports a missing source path to stderr without failing the
// caller — files.copy/files.hardlink entries are frequently gitignored
// (a developer's local config, a not-yet-generated build artifact) and a
// legitimately absent source must not abort the whole `ct create`.
func warnMissing(src string) {
	_, _ = fmt.Fprintf(os.Stderr, "ct: warning: %s does not exist, skipping\n", src)
}
