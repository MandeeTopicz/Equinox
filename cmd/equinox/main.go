// Command equinox is the CLI entrypoint. Subcommands are wired in as each
// pipeline/view stage is implemented.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: equinox <fetch|match|route|run|show> [flags]")
		os.Exit(1)
	}

	switch os.Args[1] {
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
