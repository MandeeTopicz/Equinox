package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"equinox/internal/match"
	"equinox/internal/store"
	"equinox/internal/venue"
)

// RunStore is every store capability the full fetch->match->route pipeline
// needs.
type RunStore interface {
	FetchStore
	MatchStore
	RouteStore
	ListLatestMatchDecisions(ctx context.Context) ([]store.MatchDecision, error)
}

// RunDeps holds run's constructed dependencies.
type RunDeps struct {
	Venues     []venue.VenueClient
	Store      RunStore
	Embedder   match.Embedder
	MinScore   float64
	DateWindow time.Duration
	Event      string // optional; empty means auto-select the highest-confidence match
	Side       string
	Size       float64
	Out        io.Writer
}

// Run wraps fetch -> match -> route in one command (docs/ARCHITECTURE.md).
// When Event is empty, it auto-selects the highest-confidence match group
// and logs that it did so, e.g.:
//
//	no --event given, defaulting to highest-confidence match: fed-march-2026-cut, score 0.91
//
// If match finds no groups at all, Run stops after fetch+match — there is
// nothing to route.
func Run(ctx context.Context, deps RunDeps) error {
	if err := Fetch(ctx, FetchDeps{Venues: deps.Venues, Store: deps.Store, Out: deps.Out}); err != nil {
		return err
	}

	if err := Match(ctx, MatchDeps{
		Store: deps.Store, Embedder: deps.Embedder, MinScore: deps.MinScore, DateWindow: deps.DateWindow, Out: deps.Out,
	}); err != nil {
		return err
	}

	eventID := deps.Event
	if eventID == "" {
		groups, err := deps.Store.ListLatestMatchDecisions(ctx)
		if err != nil {
			return fmt.Errorf("listing match decisions: %w", err)
		}
		if len(groups) == 0 {
			fmt.Fprintln(deps.Out, "no cross-venue matches found; nothing to route")
			return nil
		}

		// ListLatestMatchDecisions is ordered by score descending.
		best := groups[0]
		eventID = best.EventID
		fmt.Fprintf(deps.Out, "no --event given, defaulting to highest-confidence match: %s, score %.2f\n", eventID, best.Score)
	}

	return Route(ctx, RouteDeps{Store: deps.Store, Out: deps.Out}, eventID, deps.Side, deps.Size)
}
