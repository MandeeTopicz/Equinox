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
	case "run":
		return runRun(cfg, args)
	case "show":
		return runShow(cfg, args)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func openStore(cfg *Config) (*store.Store, error) {
	st, err := store.Open(cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	return st, nil
}

func newEmbedder(cfg *Config) (match.Embedder, error) {
	apiKey, err := cfg.Match.Embedding.APIKey()
	if err != nil {
		return nil, err
	}
	return match.NewOpenAIEmbeddingClient(apiKey, cfg.Match.Embedding.Model, &http.Client{Timeout: httpClientTimeout}), nil
}

func newEntityExtractor(cfg *Config) (match.EntityExtractor, error) {
	apiKey, err := cfg.Match.EntityExtraction.APIKey()
	if err != nil {
		return nil, err
	}
	return match.NewOpenAIEntityExtractor(apiKey, cfg.Match.EntityExtraction.Model, &http.Client{Timeout: httpClientTimeout}), nil
}

func runFetch(cfg *Config) error {
	st, err := openStore(cfg)
	if err != nil {
		return err
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
	verbose := fs.Bool("verbose", false, "print a trace of every candidate pair considered, including gate rejections")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	embedder, err := newEmbedder(cfg)
	if err != nil {
		return err
	}
	extractor, err := newEntityExtractor(cfg)
	if err != nil {
		return err
	}

	return cli.Match(context.Background(), cli.MatchDeps{
		Store:      st,
		Embedder:   embedder,
		Extractor:  extractor,
		DateWindow: match.DefaultDateWindow,
		Verbose:    *verbose,
		Out:        os.Stdout,
	})
}

func runRoute(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("route", flag.ContinueOnError)
	event := fs.String("event", "", "match-group event id (see `equinox show matches`)")
	side := fs.String("side", "", `"yes" or "no"`)
	size := fs.Int("size", 0, "hypothetical contract count")
	confirmReview := fs.Bool("confirm-review", false, `route a "needs review" tier event anyway (see docs/EQUIVALENCE.md)`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *event == "" {
		return fmt.Errorf("--event is required (see `equinox show matches` for available event ids)")
	}

	st, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	return cli.Route(context.Background(), cli.RouteDeps{Store: st, Out: os.Stdout}, *event, *side, float64(*size), *confirmReview)
}

func runRun(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	event := fs.String("event", "", "match-group event id; if omitted, defaults to the highest-confidence match found")
	side := fs.String("side", "yes", `"yes" or "no"`)
	size := fs.Int("size", 100, "hypothetical contract count")
	confirmReview := fs.Bool("confirm-review", false, `route an explicit --event that's only "needs review" tier anyway`)
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	clients, err := buildVenueClients(cfg)
	if err != nil {
		return err
	}

	embedder, err := newEmbedder(cfg)
	if err != nil {
		return err
	}
	extractor, err := newEntityExtractor(cfg)
	if err != nil {
		return err
	}

	return cli.Run(context.Background(), cli.RunDeps{
		Venues:        clients,
		Store:         st,
		Embedder:      embedder,
		Extractor:     extractor,
		DateWindow:    match.DefaultDateWindow,
		Event:         *event,
		Side:          *side,
		Size:          float64(*size),
		ConfirmReview: *confirmReview,
		Out:           os.Stdout,
	})
}

func runShow(cfg *Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: equinox show <markets|matches|decisions> [flags]")
	}
	subcommand, rest := args[0], args[1:]

	st, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	switch subcommand {
	case "markets":
		fs := flag.NewFlagSet("show markets", flag.ContinueOnError)
		venueFilter := fs.String("venue", "", "filter to one venue")
		jsonOutput := fs.Bool("json", false, "output as JSON")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return cli.ShowMarkets(context.Background(), st, os.Stdout, *venueFilter, *jsonOutput)

	case "matches":
		fs := flag.NewFlagSet("show matches", flag.ContinueOnError)
		eventFilter := fs.String("event", "", "filter to one event id")
		jsonOutput := fs.Bool("json", false, "output as JSON")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return cli.ShowMatches(context.Background(), st, os.Stdout, *eventFilter, *jsonOutput)

	case "decisions":
		fs := flag.NewFlagSet("show decisions", flag.ContinueOnError)
		eventFilter := fs.String("event", "", "filter to one event id")
		jsonOutput := fs.Bool("json", false, "output as JSON")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return cli.ShowDecisions(context.Background(), st, os.Stdout, *eventFilter, *jsonOutput)

	default:
		return fmt.Errorf("unknown show subcommand: %s (want markets, matches, or decisions)", subcommand)
	}
}
