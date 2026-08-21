package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"equinox/internal/match"
	"equinox/internal/normalize"
	"equinox/internal/route"
	"equinox/internal/store"
)

// RouteStore is the subset of *store.Store the route command needs.
type RouteStore interface {
	LatestMatchDecision(ctx context.Context, eventID string) (store.MatchDecision, error)
	GetCanonicalMarket(ctx context.Context, id string) (store.CanonicalMarket, error)
	InsertRoutingDecision(ctx context.Context, d store.RoutingDecision) (int64, error)
}

// RouteDeps holds route's constructed dependencies.
type RouteDeps struct {
	Store RouteStore
	Out   io.Writer
}

// Route simulates a hypothetical order for eventID (see docs/ROUTING.md).
// eventID is resolved first as a match-group event id; if none exists, as
// a raw canonical market id, logged as a single-venue no-op since there is
// nothing to compare against. If neither resolves, that's a real error —
// see docs/ROUTING.md's "If --event refers to..." clarification.
//
// A matched-group event whose tier is "needs review" (docs/EQUIVALENCE.md)
// refuses to route unless confirmReview is true — routing a group the
// matcher itself flagged as not confident enough shouldn't be something
// that happens by accident.
func Route(ctx context.Context, deps RouteDeps, eventID, side string, size float64, confirmReview bool) error {
	if eventID == "" {
		return fmt.Errorf("event id is required")
	}
	if side != "yes" && side != "no" {
		return fmt.Errorf("invalid side %q: must be \"yes\" or \"no\"", side)
	}
	if size <= 0 {
		return fmt.Errorf("size must be positive, got %v", size)
	}

	md, err := deps.Store.LatestMatchDecision(ctx, eventID)
	switch {
	case err == nil:
		return routeMatchedGroup(ctx, deps, md, side, size, confirmReview)
	case errors.Is(err, sql.ErrNoRows):
		return routeSingleMarketNoop(ctx, deps, eventID, side, size)
	default:
		return fmt.Errorf("looking up match decision for %s: %w", eventID, err)
	}
}

func routeMatchedGroup(ctx context.Context, deps RouteDeps, md store.MatchDecision, side string, size float64, confirmReview bool) error {
	tier := match.ClassifyTier(md.TitleSimilarity, md.DateAlignment)
	if tier != match.TierMatched && !confirmReview {
		return fmt.Errorf("event %q is %q (score %.2f), not \"matched\" — not confident enough to route automatically; re-run with --confirm-review to route anyway, acknowledging this hasn't cleared the matched threshold", md.EventID, tier, md.Score)
	}

	var members []normalize.Market
	for _, mm := range md.Members {
		cm, err := deps.Store.GetCanonicalMarket(ctx, mm.CanonicalMarketID)
		if errors.Is(err, sql.ErrNoRows) {
			continue // no longer in current state (e.g. closed since fetch); skip gracefully
		}
		if err != nil {
			return fmt.Errorf("loading canonical market %s: %w", mm.CanonicalMarketID, err)
		}
		members = append(members, toNormalizeMarket(cm))
	}
	if len(members) == 0 {
		return fmt.Errorf("no current market data available for event %s (all matched markets have since disappeared)", md.EventID)
	}

	decision, err := route.Route(members, side, size)
	if err != nil {
		return fmt.Errorf("routing: %w", err)
	}

	comparisonJSON, err := json.Marshal(decision.Quotes)
	if err != nil {
		return fmt.Errorf("marshaling comparison: %w", err)
	}

	var selectedVenue sql.NullString
	if decision.SelectedVenue != "" {
		selectedVenue = sql.NullString{String: decision.SelectedVenue, Valid: true}
	}

	_, err = deps.Store.InsertRoutingDecision(ctx, store.RoutingDecision{
		MatchDecisionID: sql.NullInt64{Int64: md.ID, Valid: true},
		EventID:         md.EventID,
		Side:            side,
		Size:            size,
		CreatedAt:       time.Now().UTC(),
		SelectedVenue:   selectedVenue,
		IsNoop:          false,
		Rationale:       decision.Rationale,
		ComparisonJSON:  string(comparisonJSON),
	})
	if err != nil {
		return fmt.Errorf("storing routing decision: %w", err)
	}

	fmt.Fprintln(deps.Out, decision.Rationale)
	return nil
}

func routeSingleMarketNoop(ctx context.Context, deps RouteDeps, eventID, side string, size float64) error {
	cm, err := deps.Store.GetCanonicalMarket(ctx, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no match group or market found for event %q (see `equinox show matches` and `equinox show markets`)", eventID)
	}
	if err != nil {
		return fmt.Errorf("loading market %s: %w", eventID, err)
	}

	m := toNormalizeMarket(cm)
	price, err := m.Price(side)
	if err != nil {
		return fmt.Errorf("routing: %w", err)
	}

	rationale := fmt.Sprintf("no match group for %s; single-venue no-op — only %s available (%s price %.2f)", eventID, cm.Venue, side, price)

	quotes := []route.VenueQuote{{Venue: cm.Venue, Price: price, Liquidity: cm.Liquidity, LiquidityOK: cm.Liquidity >= size}}
	comparisonJSON, err := json.Marshal(quotes)
	if err != nil {
		return fmt.Errorf("marshaling comparison: %w", err)
	}

	_, err = deps.Store.InsertRoutingDecision(ctx, store.RoutingDecision{
		EventID:        eventID,
		Side:           side,
		Size:           size,
		CreatedAt:      time.Now().UTC(),
		IsNoop:         true,
		Rationale:      rationale,
		ComparisonJSON: string(comparisonJSON),
	})
	if err != nil {
		return fmt.Errorf("storing routing decision: %w", err)
	}

	fmt.Fprintln(deps.Out, rationale)
	return nil
}
