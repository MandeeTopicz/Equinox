package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"equinox/internal/match"
	"equinox/internal/store"
)

type fakeMatchEmbedder struct {
	vectors map[string][]float64
}

func (f fakeMatchEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		out[i] = f.vectors[t]
	}
	return out, nil
}

// fakeMatchEntityExtractor returns no entities for anything by default —
// the zero value never blocks a candidate pair on the entity gate.
type fakeMatchEntityExtractor struct {
	byText map[string][]string
}

func (f fakeMatchEntityExtractor) ExtractEntities(ctx context.Context, text string) ([]string, error) {
	return f.byText[text], nil
}

type fakeMatchStore struct {
	markets   []store.CanonicalMarket
	inserted  []store.MatchDecision
	listErr   error
	insertErr error
}

func (f *fakeMatchStore) ListCanonicalMarkets(ctx context.Context, venue string) ([]store.CanonicalMarket, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.markets, nil
}

func (f *fakeMatchStore) InsertMatchDecision(ctx context.Context, d store.MatchDecision) (int64, error) {
	if f.insertErr != nil {
		return 0, f.insertErr
	}
	f.inserted = append(f.inserted, d)
	return int64(len(f.inserted)), nil
}

func TestMatchCommandInsertsGroupsAndPrintsSummary(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	st := &fakeMatchStore{markets: []store.CanonicalMarket{
		{ID: "polymarket:1", Venue: "polymarket", VenueMarketID: "1", Title: "Will the Fed cut rates?", ResolutionDate: base},
		{ID: "kalshi:1", Venue: "kalshi", VenueMarketID: "1", Title: "Fed rate cut", Category: "econ", ResolutionDate: base},
	}}
	embedder := fakeMatchEmbedder{vectors: map[string][]float64{
		"Will the Fed cut rates?": {1, 0},
		"Fed rate cut":            {1, 0}, // identical -> similarity 1.0
	}}
	var out bytes.Buffer

	err := Match(context.Background(), MatchDeps{
		Store: st, Embedder: embedder, Extractor: fakeMatchEntityExtractor{}, MinScore: 0.75, DateWindow: match.DefaultDateWindow, Out: &out,
	})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}

	if len(st.inserted) != 1 {
		t.Fatalf("expected 1 match decision inserted, got %d", len(st.inserted))
	}
	d := st.inserted[0]
	// Members are sorted by (venue, venue_market_id); "kalshi" sorts before
	// "polymarket", so the kalshi market is Members[0] and the slug anchor.
	if d.EventID != "fed-rate-cut" {
		t.Errorf("EventID = %q, want fed-rate-cut", d.EventID)
	}
	if d.MinScore != 0.75 {
		t.Errorf("MinScore = %v, want 0.75", d.MinScore)
	}
	if len(d.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(d.Members))
	}
	if d.SignalsJSON == "" || d.SignalsJSON == "null" {
		t.Errorf("expected a non-empty signals breakdown, got %q", d.SignalsJSON)
	}

	wantSummary := "matched 1 cross-venue groups (min-score 0.75)\n"
	if out.String() != wantSummary {
		t.Errorf("summary = %q, want %q", out.String(), wantSummary)
	}
}

func TestMatchCommandNoMarketsYet(t *testing.T) {
	st := &fakeMatchStore{}
	var out bytes.Buffer

	err := Match(context.Background(), MatchDeps{Store: st, Embedder: fakeMatchEmbedder{}, Extractor: fakeMatchEntityExtractor{}, MinScore: 0.75, DateWindow: match.DefaultDateWindow, Out: &out})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(st.inserted) != 0 {
		t.Errorf("expected no decisions inserted, got %d", len(st.inserted))
	}
	if !strings.Contains(out.String(), "no canonical markets yet") {
		t.Errorf("expected a no-data message, got %q", out.String())
	}
}

func TestMatchCommandDedupesSlugsWithinOneRun(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	far := base.Add(100 * match.DefaultDateWindow)

	// Two separate groups whose anchor markets (kalshi sorts before
	// polymarket, so it's first in each group's Members) slugify to the
	// same string.
	st := &fakeMatchStore{markets: []store.CanonicalMarket{
		{ID: "kalshi:1", Venue: "kalshi", VenueMarketID: "1", Title: "Same Title", ResolutionDate: base},
		{ID: "polymarket:1", Venue: "polymarket", VenueMarketID: "1", Title: "Polymarket A", ResolutionDate: base},
		{ID: "kalshi:2", Venue: "kalshi", VenueMarketID: "2", Title: "Same Title", ResolutionDate: far},
		{ID: "polymarket:2", Venue: "polymarket", VenueMarketID: "2", Title: "Polymarket B", ResolutionDate: far},
	}}
	embedder := fakeMatchEmbedder{vectors: map[string][]float64{
		"Same Title":   {1, 0},
		"Polymarket A": {1, 0},
		"Polymarket B": {1, 0},
	}}
	var out bytes.Buffer

	err := Match(context.Background(), MatchDeps{Store: st, Embedder: embedder, Extractor: fakeMatchEntityExtractor{}, MinScore: 0.75, DateWindow: match.DefaultDateWindow, Out: &out})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(st.inserted) != 2 {
		t.Fatalf("expected 2 match decisions, got %d", len(st.inserted))
	}
	if st.inserted[0].EventID == st.inserted[1].EventID {
		t.Errorf("expected distinct event ids, both were %q", st.inserted[0].EventID)
	}
}
