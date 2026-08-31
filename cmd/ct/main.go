// Command ct is a zero-config git worktree manager.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/luca-cattaneo/clone-tree/internal/cli"
	"github.com/luca-cattaneo/clone-tree/internal/config"
)

func main() {
	err := cli.Execute()
	if err == nil {
		return
	}

	// ScaffoldedError renders its own multi-line, colorized checklist — the
	// usual "ct: " prefix would land in front of the first line only and
	// break the block's readability. cli.ErrChecksFailed is `ct doctor`'s
	// sentinel for "the report I already printed to stdout found a ✗" — it
	// carries nothing to add, so it prints nothing further.
	var scaffolded *config.ScaffoldedError
	switch {
	case errors.As(err, &scaffolded):
		fmt.Fprintln(os.Stderr, scaffolded.Error())
	case errors.Is(err, cli.ErrChecksFailed):
		// doctor's own report already explains the failure.
	default:
		fmt.Fprintf(os.Stderr, "ct: %v\n", err)
	}
	os.Exit(1)
}
