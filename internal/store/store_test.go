package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "equinox.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenAppliesSchema(t *testing.T) {
	s := openTestStore(t)

	tables := []string{"raw_markets", "canonical_markets", "match_decisions", "routing_decisions"}
	for _, table := range tables {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
}

func TestReplaceVenueMarkets(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	poly := []CanonicalMarket{
		{ID: "polymarket:1", Venue: "polymarket", VenueMarketID: "1", Title: "Fed cuts in March?",
			Category: "econ", ResolutionDate: now.AddDate(0, 0, 30), YesPrice: 0.65, NoPrice: 0.35, Liquidity: 1000, FetchedAt: now},
	}
	polyRaw := []RawMarket{{Venue: "polymarket", VenueMarketID: "1", RawJSON: `{"id":"1"}`, FetchedAt: now}}

	if err := s.ReplaceVenueMarkets(ctx, "polymarket", polyRaw, poly); err != nil {
		t.Fatalf("ReplaceVenueMarkets(polymarket): %v", err)
	}

	kalshi := []CanonicalMarket{
		{ID: "kalshi:FED-MAR", Venue: "kalshi", VenueMarketID: "FED-MAR", Title: "March FOMC: rate cut?",
			Category: "econ", ResolutionDate: now.AddDate(0, 0, 30), YesPrice: 0.62, NoPrice: 0.38, Liquidity: 500, FetchedAt: now},
	}
	kalshiRaw := []RawMarket{{Venue: "kalshi", VenueMarketID: "FED-MAR", RawJSON: `{"ticker":"FED-MAR"}`, FetchedAt: now}}

	if err := s.ReplaceVenueMarkets(ctx, "kalshi", kalshiRaw, kalshi); err != nil {
		t.Fatalf("ReplaceVenueMarkets(kalshi): %v", err)
	}

	all, err := s.ListCanonicalMarkets(ctx, "")
	if err != nil {
		t.Fatalf("ListCanonicalMarkets(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 canonical markets across venues, got %d", len(all))
	}

	polyOnly, err := s.ListCanonicalMarkets(ctx, "polymarket")
	if err != nil {
		t.Fatalf("ListCanonicalMarkets(polymarket): %v", err)
	}
	if len(polyOnly) != 1 || polyOnly[0].ID != "polymarket:1" {
		t.Fatalf("expected 1 polymarket market, got %+v", polyOnly)
	}

	got, err := s.GetCanonicalMarket(ctx, "polymarket:1")
	if err != nil {
		t.Fatalf("GetCanonicalMarket: %v", err)
	}
	if got.Title != "Fed cuts in March?" || got.YesPrice != 0.65 {
		t.Errorf("GetCanonicalMarket returned unexpected data: %+v", got)
	}
	if !got.ResolutionDate.Equal(now.AddDate(0, 0, 30)) {
		t.Errorf("ResolutionDate round-trip mismatch: got %v", got.ResolutionDate)
	}

	// Re-fetching polymarket with a different market should drop the old one
	// (state tables represent current knowledge, not history).
	polyV2 := []CanonicalMarket{
		{ID: "polymarket:2", Venue: "polymarket", VenueMarketID: "2", Title: "Different market",
			ResolutionDate: now.AddDate(0, 0, 10), YesPrice: 0.5, NoPrice: 0.5, Liquidity: 200, FetchedAt: now},
	}
	polyV2Raw := []RawMarket{{Venue: "polymarket", VenueMarketID: "2", RawJSON: `{"id":"2"}`, FetchedAt: now}}
	if err := s.ReplaceVenueMarkets(ctx, "polymarket", polyV2Raw, polyV2); err != nil {
		t.Fatalf("ReplaceVenueMarkets(polymarket v2): %v", err)
	}

	if _, err := s.GetCanonicalMarket(ctx, "polymarket:1"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected polymarket:1 to be gone after replace, got err=%v", err)
	}
	stillThere, err := s.GetCanonicalMarket(ctx, "kalshi:FED-MAR")
	if err != nil || stillThere.ID != "kalshi:FED-MAR" {
		t.Errorf("kalshi market should be untouched by a polymarket replace, got %+v, err=%v", stillThere, err)
	}
}

func TestGetCanonicalMarketNotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetCanonicalMarket(context.Background(), "does-not-exist")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestMatchDecisions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	t1 := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)

	members := []MatchMember{
		{Venue: "polymarket", CanonicalMarketID: "polymarket:1", Title: "Fed cuts in March?"},
		{Venue: "kalshi", CanonicalMarketID: "kalshi:FED-MAR", Title: "March FOMC: rate cut?"},
	}

	first := MatchDecision{
		EventID: "fed-march-2026-cut", CreatedAt: t1, MinScore: 0.75, Score: 0.88,
		TitleSimilarity: 0.9, DateAlignment: 1.0, CategoryMatch: 1.0,
		Members: members, SignalsJSON: `[{"pair":"polymarket:1,kalshi:FED-MAR","score":0.88}]`,
	}
	firstID, err := s.InsertMatchDecision(ctx, first)
	if err != nil {
		t.Fatalf("InsertMatchDecision(first): %v", err)
	}

	second := first
	second.CreatedAt = t2
	second.Score = 0.91
	if _, err := s.InsertMatchDecision(ctx, second); err != nil {
		t.Fatalf("InsertMatchDecision(second): %v", err)
	}

	latest, err := s.LatestMatchDecision(ctx, "fed-march-2026-cut")
	if err != nil {
		t.Fatalf("LatestMatchDecision: %v", err)
	}
	if latest.Score != 0.91 {
		t.Errorf("expected latest score 0.91, got %v", latest.Score)
	}
	if len(latest.Members) != 2 || latest.Members[0].Venue != "polymarket" {
		t.Errorf("members round-trip mismatch: %+v", latest.Members)
	}

	all, err := s.ListLatestMatchDecisions(ctx)
	if err != nil {
		t.Fatalf("ListLatestMatchDecisions: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 event (deduped to latest), got %d", len(all))
	}
	if all[0].ID != firstID+1 { // the second insert's row
		t.Errorf("expected latest row id %d, got %d", firstID+1, all[0].ID)
	}

	if _, err := s.LatestMatchDecision(ctx, "no-such-event"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for unknown event, got %v", err)
	}
}

func TestRoutingDecisions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	md, err := s.InsertMatchDecision(ctx, MatchDecision{
		EventID: "fed-march-2026-cut", CreatedAt: now, MinScore: 0.75, Score: 0.91,
		Members:     []MatchMember{{Venue: "kalshi", CanonicalMarketID: "kalshi:FED-MAR", Title: "x"}},
		SignalsJSON: `[]`,
	})
	if err != nil {
		t.Fatalf("InsertMatchDecision: %v", err)
	}

	routed := RoutingDecision{
		MatchDecisionID: sql.NullInt64{Int64: md, Valid: true},
		EventID:         "fed-march-2026-cut",
		Side:            "yes",
		Size:            100,
		CreatedAt:       now,
		SelectedVenue:   sql.NullString{String: "kalshi", Valid: true},
		IsNoop:          false,
		Rationale:       "selected: kalshi — best YES price at requested size (0.62 vs. 0.65); manifold excluded on liquidity",
		ComparisonJSON:  `[{"venue":"kalshi","price":0.62,"liquidity_ok":true,"selected":true}]`,
	}
	if _, err := s.InsertRoutingDecision(ctx, routed); err != nil {
		t.Fatalf("InsertRoutingDecision: %v", err)
	}

	noop := RoutingDecision{
		EventID:        "solo-market",
		Side:           "no",
		Size:           50,
		CreatedAt:      now.Add(time.Minute),
		IsNoop:         true,
		Rationale:      "no match group for this event; single-venue no-op",
		ComparisonJSON: `[]`,
	}
	if _, err := s.InsertRoutingDecision(ctx, noop); err != nil {
		t.Fatalf("InsertRoutingDecision(noop): %v", err)
	}

	forEvent, err := s.ListRoutingDecisions(ctx, "fed-march-2026-cut")
	if err != nil {
		t.Fatalf("ListRoutingDecisions(event): %v", err)
	}
	if len(forEvent) != 1 || !forEvent[0].SelectedVenue.Valid || forEvent[0].SelectedVenue.String != "kalshi" {
		t.Fatalf("unexpected routing decision for event: %+v", forEvent)
	}
	if !forEvent[0].MatchDecisionID.Valid || forEvent[0].MatchDecisionID.Int64 != md {
		t.Errorf("expected match_decision_id %d, got %+v", md, forEvent[0].MatchDecisionID)
	}

	all, err := s.ListRoutingDecisions(ctx, "")
	if err != nil {
		t.Fatalf("ListRoutingDecisions(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 routing decisions total, got %d", len(all))
	}

	var noopDecision RoutingDecision
	for _, d := range all {
		if d.EventID == "solo-market" {
			noopDecision = d
		}
	}
	if !noopDecision.IsNoop {
		t.Error("expected solo-market decision to be marked IsNoop")
	}
	if noopDecision.MatchDecisionID.Valid {
		t.Error("expected no-op decision to have no match_decision_id")
	}
	if noopDecision.SelectedVenue.Valid {
		t.Error("expected no-op decision to have no selected venue")
	}
}
