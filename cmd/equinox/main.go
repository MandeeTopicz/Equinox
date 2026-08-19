// Command equinox is the CLI entrypoint. Subcommands are wired in as each
// pipeline/view stage is implemented.
package main

import (
	"context"
	"fmt"
	"os"

	"equinox/internal/cli"
	"equinox/internal/store"
)

const (
	configPath = "equinox.yaml"
	envPath    = ".env"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: equinox <fetch|match|route|run|show> [flags]")
		os.Exit(1)
	}

	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(command string, args []string) error {
	if err := LoadEnv(envPath); err != nil {
		return fmt.Errorf("loading .env: %w", err)
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}

	switch command {
	case "fetch":
		return runFetch(cfg)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func runFetch(cfg *Config) error {
	st, err := store.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	clients, err := buildVenueClients(cfg)
	if err != nil {
		return err
	}

	return cli.Fetch(context.Background(), cli.FetchDeps{Venues: clients, Store: st, Out: os.Stdout})
}
