// Command equinox is the CLI entrypoint. Subcommands are wired in as each
// pipeline/view stage is implemented.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"equinox/internal/cli"
	"equinox/internal/match"
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
	case "match":
		return runMatch(cfg, args)
	case "route":
		return runRoute(cfg, args)
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

func runMatch(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("match", flag.ContinueOnError)
	minScore := fs.Float64("min-score", cfg.Match.MinScore, "minimum composite score to consider two markets equivalent")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := store.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	apiKey, err := cfg.Match.Embedding.APIKey()
	if err != nil {
		return err
	}
	embedder := match.NewOpenAIEmbeddingClient(apiKey, cfg.Match.Embedding.Model, &http.Client{Timeout: httpClientTimeout})

	return cli.Match(context.Background(), cli.MatchDeps{
		Store:      st,
		Embedder:   embedder,
		MinScore:   *minScore,
		DateWindow: match.DefaultDateWindow,
		Out:        os.Stdout,
	})
}

func runRoute(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("route", flag.ContinueOnError)
	event := fs.String("event", "", "match-group event id (see `equinox show matches`)")
	side := fs.String("side", "", `"yes" or "no"`)
	size := fs.Int("size", 0, "hypothetical contract count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *event == "" {
		return fmt.Errorf("--event is required (see `equinox show matches` for available event ids)")
	}

	st, err := store.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	return cli.Route(context.Background(), cli.RouteDeps{Store: st, Out: os.Stdout}, *event, *side, float64(*size))
}
