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
