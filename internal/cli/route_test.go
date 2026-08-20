package cli

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"equinox/internal/store"
)

type fakeRouteStore struct {
	matchDecisions   map[string]store.MatchDecision
	canonicalMarkets map[string]store.CanonicalMarket
	inserted         []store.RoutingDecision
}

func (f *fakeRouteStore) LatestMatchDecision(ctx context.Context, eventID string) (store.MatchDecision, error) {
	d, ok := f.matchDecisions[eventID]
	if !ok {
		return store.MatchDecision{}, sql.ErrNoRows
	}
	return d, nil
}

func (f *fakeRouteStore) GetCanonicalMarket(ctx context.Context, id string) (store.CanonicalMarket, error) {
	m, ok := f.canonicalMarkets[id]
	if !ok {
		return store.CanonicalMarket{}, sql.ErrNoRows
	}
	return m, nil
}

func (f *fakeRouteStore) InsertRoutingDecision(ctx context.Context, d store.RoutingDecision) (int64, error) {
	f.inserted = append(f.inserted, d)
	return int64(len(f.inserted)), nil
}

func TestRouteMatchedGroup(t *testing.T) {
	now := time.Now()
	st := &fakeRouteStore{
		matchDecisions: map[string]store.MatchDecision{
			"fed-march-2026-cut": {
				ID: 1, EventID: "fed-march-2026-cut",
				Members: []store.MatchMember{
					{Venue: "kalshi", CanonicalMarketID: "kalshi:1"},
					{Venue: "polymarket", CanonicalMarketID: "polymarket:1"},
					{Venue: "manifold", CanonicalMarketID: "manifold:1"},
				},
			},
		},
		canonicalMarkets: map[string]store.CanonicalMarket{
			"kalshi:1":     {ID: "kalshi:1", Venue: "kalshi", YesPrice: 0.62, NoPrice: 0.38, Liquidity: 1000, ResolutionDate: now},
			"polymarket:1": {ID: "polymarket:1", Venue: "polymarket", YesPrice: 0.65, NoPrice: 0.35, Liquidity: 1000, ResolutionDate: now},
			"manifold:1":   {ID: "manifold:1", Venue: "manifold", YesPrice: 0.60, NoPrice: 0.40, Liquidity: 50, ResolutionDate: now},
		},
	}
	var out bytes.Buffer

	err := Route(context.Background(), RouteDeps{Store: st, Out: &out}, "fed-march-2026-cut", "yes", 100)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	if len(st.inserted) != 1 {
		t.Fatalf("expected 1 routing decision inserted, got %d", len(st.inserted))
	}
	d := st.inserted[0]
	if !d.SelectedVenue.Valid || d.SelectedVenue.String != "kalshi" {
		t.Errorf("SelectedVenue = %+v, want kalshi", d.SelectedVenue)
	}
	if !d.MatchDecisionID.Valid || d.MatchDecisionID.Int64 != 1 {
		t.Errorf("MatchDecisionID = %+v, want 1", d.MatchDecisionID)
	}
	if d.IsNoop {
		t.Error("expected IsNoop false for a real matched-group decision")
	}
	if !strings.Contains(out.String(), "selected: kalshi") {
		t.Errorf("expected stdout to contain the rationale, got %q", out.String())
	}
}

func TestRouteSingleMarketNoop(t *testing.T) {
	st := &fakeRouteStore{
		matchDecisions: map[string]store.MatchDecision{}, // no match group
		canonicalMarkets: map[string]store.CanonicalMarket{
			"kalshi:solo": {ID: "kalshi:solo", Venue: "kalshi", YesPrice: 0.40, NoPrice: 0.60, Liquidity: 10},
		},
	}
	var out bytes.Buffer

	err := Route(context.Background(), RouteDeps{Store: st, Out: &out}, "kalshi:solo", "yes", 5)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	if len(st.inserted) != 1 {
		t.Fatalf("expected 1 routing decision inserted, got %d", len(st.inserted))
	}
	d := st.inserted[0]
	if !d.IsNoop {
		t.Error("expected IsNoop true for a market with no match group")
	}
	if d.MatchDecisionID.Valid {
		t.Error("expected no match_decision_id for a no-op decision")
	}
	if d.SelectedVenue.Valid {
		t.Error("expected no selected venue for a no-op decision")
	}
	if !strings.Contains(out.String(), "single-venue no-op") {
		t.Errorf("expected a no-op message, got %q", out.String())
	}
}

func TestRouteUnknownEventIsAnError(t *testing.T) {
	st := &fakeRouteStore{}
	var out bytes.Buffer

	err := Route(context.Background(), RouteDeps{Store: st, Out: &out}, "totally-unknown", "yes", 100)
	if err == nil {
		t.Fatal("expected an error when neither a match group nor a market resolves, got nil")
	}
	if len(st.inserted) != 0 {
		t.Errorf("expected no routing decision inserted, got %d", len(st.inserted))
	}
}

func TestRouteValidatesInput(t *testing.T) {
	st := &fakeRouteStore{}
	ctx := context.Background()

	tests := []struct {
		name        string
		event, side string
		size        float64
	}{
		{"empty event", "", "yes", 100},
		{"invalid side", "x", "maybe", 100},
		{"zero size", "x", "yes", 0},
		{"negative size", "x", "yes", -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Route(ctx, RouteDeps{Store: st, Out: &out}, tt.event, tt.side, tt.size); err == nil {
				t.Error("expected a validation error, got nil")
			}
		})
	}
}

func TestRouteSkipsMissingMemberMarketsGracefully(t *testing.T) {
	now := time.Now()
	st := &fakeRouteStore{
		matchDecisions: map[string]store.MatchDecision{
			"event-1": {
				ID: 1, EventID: "event-1",
				Members: []store.MatchMember{
					{Venue: "kalshi", CanonicalMarketID: "kalshi:1"},
					{Venue: "polymarket", CanonicalMarketID: "polymarket:gone"}, // no longer in canonical_markets
				},
			},
		},
		canonicalMarkets: map[string]store.CanonicalMarket{
			"kalshi:1": {ID: "kalshi:1", Venue: "kalshi", YesPrice: 0.5, NoPrice: 0.5, Liquidity: 100, ResolutionDate: now},
		},
	}
	var out bytes.Buffer

	err := Route(context.Background(), RouteDeps{Store: st, Out: &out}, "event-1", "yes", 10)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if len(st.inserted) != 1 || !st.inserted[0].SelectedVenue.Valid || st.inserted[0].SelectedVenue.String != "kalshi" {
		t.Errorf("expected kalshi selected despite the missing member, got %+v", st.inserted)
	}
}
