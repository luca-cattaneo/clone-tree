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
	// break the block's readability.
	var scaffolded *config.ScaffoldedError
	if errors.As(err, &scaffolded) {
		fmt.Fprintln(os.Stderr, scaffolded.Error())
	} else {
		fmt.Fprintf(os.Stderr, "ct: %v\n", err)
	}
	os.Exit(1)
}
