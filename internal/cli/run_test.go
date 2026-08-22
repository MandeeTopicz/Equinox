package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"equinox/internal/normalize"
	"equinox/internal/store"
	"equinox/internal/venue"
)

// fakeRunStore is a minimal in-memory store implementing RunStore, needed
// because Run chains Fetch -> Match -> Route through the same store: data
// one stage writes must be visible to the next, unlike the narrower
// single-stage fakes in fetch_test.go/match_test.go/route_test.go.
type fakeRunStore struct {
	canonicalMarkets map[string]store.CanonicalMarket
	matchDecisions   []store.MatchDecision
	insertedRouting  []store.RoutingDecision
}

func (f *fakeRunStore) ReplaceVenueMarkets(ctx context.Context, venueName string, raw []store.RawMarket, canonical []store.CanonicalMarket) error {
	if f.canonicalMarkets == nil {
		f.canonicalMarkets = map[string]store.CanonicalMarket{}
	}
	for id, m := range f.canonicalMarkets {
		if m.Venue == venueName {
			delete(f.canonicalMarkets, id)
		}
	}
	for _, m := range canonical {
		f.canonicalMarkets[m.ID] = m
	}
	return nil
}

func (f *fakeRunStore) ListCanonicalMarkets(ctx context.Context, venueName string) ([]store.CanonicalMarket, error) {
	var out []store.CanonicalMarket
	for _, m := range f.canonicalMarkets {
		if venueName == "" || m.Venue == venueName {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeRunStore) GetCanonicalMarket(ctx context.Context, id string) (store.CanonicalMarket, error) {
	m, ok := f.canonicalMarkets[id]
	if !ok {
		return store.CanonicalMarket{}, sql.ErrNoRows
	}
	return m, nil
}

func (f *fakeRunStore) InsertMatchDecision(ctx context.Context, d store.MatchDecision) (int64, error) {
	d.ID = int64(len(f.matchDecisions) + 1)
	f.matchDecisions = append(f.matchDecisions, d)
	return d.ID, nil
}

func (f *fakeRunStore) LatestMatchDecision(ctx context.Context, eventID string) (store.MatchDecision, error) {
	var latest store.MatchDecision
	found := false
	for _, d := range f.matchDecisions {
		if d.EventID == eventID && (!found || d.CreatedAt.After(latest.CreatedAt)) {
			latest, found = d, true
		}
	}
	if !found {
		return store.MatchDecision{}, sql.ErrNoRows
	}
	return latest, nil
}

func (f *fakeRunStore) ListLatestMatchDecisions(ctx context.Context) ([]store.MatchDecision, error) {
	latestByEvent := map[string]store.MatchDecision{}
	for _, d := range f.matchDecisions {
		cur, ok := latestByEvent[d.EventID]
		if !ok || d.CreatedAt.After(cur.CreatedAt) {
			latestByEvent[d.EventID] = d
		}
	}
	out := make([]store.MatchDecision, 0, len(latestByEvent))
	for _, d := range latestByEvent {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

func (f *fakeRunStore) InsertRoutingDecision(ctx context.Context, d store.RoutingDecision) (int64, error) {
	f.insertedRouting = append(f.insertedRouting, d)
	return int64(len(f.insertedRouting)), nil
}

func matchingVenueClients(base time.Time) []venue.VenueClient {
	kalshi := fakeVenueClient{name: "kalshi", markets: []venue.FetchedMarket{{
		RawJSON: `{}`,
		Canonical: normalize.Market{
			ID: "kalshi:1", Venue: "kalshi", VenueMarketID: "1", Title: "Common Title", Category: "econ",
			ResolutionDate: base, YesPrice: 0.62, NoPrice: 0.38, Liquidity: 1000, FetchedAt: base,
		},
	}}}
	polymarket := fakeVenueClient{name: "polymarket", markets: []venue.FetchedMarket{{
		RawJSON: `{}`,
		Canonical: normalize.Market{
			ID: "polymarket:1", Venue: "polymarket", VenueMarketID: "1", Title: "Common Title",
			ResolutionDate: base, YesPrice: 0.65, NoPrice: 0.35, Liquidity: 1000, FetchedAt: base,
		},
	}}}
	return []venue.VenueClient{kalshi, polymarket}
}

func TestRunAutoSelectsHighestConfidenceMatch(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	st := &fakeRunStore{}
	var out bytes.Buffer

	err := Run(context.Background(), RunDeps{
		Venues:     matchingVenueClients(base),
		Store:      st,
		Embedder:   fakeMatchEmbedder{vectors: map[string][]float64{"Common Title": {1, 0}}},
		Extractor:  fakeMatchEntityExtractor{},
		DateWindow: 48 * time.Hour,
		Side:       "yes",
		Size:       10,
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "fetched 1 markets from kalshi, 1 from polymarket") {
		t.Errorf("expected fetch summary, got %q", output)
	}
	if !strings.Contains(output, "matched 1 cross-venue groups") {
		t.Errorf("expected match summary, got %q", output)
	}
	if !strings.Contains(output, "no --event given, defaulting to highest-confidence match:") {
		t.Errorf("expected auto-select message, got %q", output)
	}
	if !strings.Contains(output, "selected: kalshi") {
		t.Errorf("expected routing rationale (kalshi has the better YES price), got %q", output)
	}

	if len(st.insertedRouting) != 1 {
		t.Fatalf("expected 1 routing decision, got %d", len(st.insertedRouting))
	}
}

func TestRunWithExplicitEventSkipsAutoSelect(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	solo := fakeVenueClient{name: "kalshi", markets: []venue.FetchedMarket{{
		RawJSON: `{}`,
		Canonical: normalize.Market{
			ID: "kalshi:solo", Venue: "kalshi", VenueMarketID: "solo", Title: "Solo market",
			ResolutionDate: base, YesPrice: 0.4, NoPrice: 0.6, Liquidity: 100, FetchedAt: base,
		},
	}}}

	st := &fakeRunStore{}
	var out bytes.Buffer

	err := Run(context.Background(), RunDeps{
		Venues:     []venue.VenueClient{solo},
		Store:      st,
		Embedder:   fakeMatchEmbedder{},
		Extractor:  fakeMatchEntityExtractor{},
		DateWindow: 48 * time.Hour,
		Event:      "kalshi:solo",
		Side:       "yes",
		Size:       10,
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if strings.Contains(out.String(), "no --event given") {
		t.Errorf("did not expect the auto-select message when --event is given, got %q", out.String())
	}
	if !strings.Contains(out.String(), "single-venue no-op") {
		t.Errorf("expected the explicit event to be routed, got %q", out.String())
	}
}

func TestRunNoMatchesFoundStopsGracefully(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	// Two markets whose titles produce orthogonal (dissimilar) vectors, so
	// no pair clears any reasonable threshold.
	kalshi := fakeVenueClient{name: "kalshi", markets: []venue.FetchedMarket{{
		RawJSON: `{}`,
		Canonical: normalize.Market{
			ID: "kalshi:1", Venue: "kalshi", VenueMarketID: "1", Title: "A", ResolutionDate: base,
			YesPrice: 0.5, NoPrice: 0.5, Liquidity: 100, FetchedAt: base,
		},
	}}}
	polymarket := fakeVenueClient{name: "polymarket", markets: []venue.FetchedMarket{{
		RawJSON: `{}`,
		Canonical: normalize.Market{
			ID: "polymarket:1", Venue: "polymarket", VenueMarketID: "1", Title: "B", ResolutionDate: base,
			YesPrice: 0.5, NoPrice: 0.5, Liquidity: 100, FetchedAt: base,
		},
	}}}

	st := &fakeRunStore{}
	var out bytes.Buffer

	err := Run(context.Background(), RunDeps{
		Venues:     []venue.VenueClient{kalshi, polymarket},
		Store:      st,
		Embedder:   fakeMatchEmbedder{vectors: map[string][]float64{"A": {1, 0}, "B": {0, 1}}},
		Extractor:  fakeMatchEntityExtractor{},
		DateWindow: 48 * time.Hour,
		Side:       "yes",
		Size:       10,
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "no cross-venue matches found; nothing to route") {
		t.Errorf("expected the no-matches message, got %q", out.String())
	}
	if len(st.insertedRouting) != 0 {
		t.Errorf("expected no routing decision when there's nothing to route, got %d", len(st.insertedRouting))
	}
}

func TestRunAutoSelectSkipsNeedsReviewTier(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	// Vectors chosen for an exact 0.75 cosine similarity — comfortably in
	// the review band (>=0.65) but below the matched title floor (0.80).
	// Same date on both sides clears the date floor for either tier, so
	// title alone decides the tier here.
	kalshi := fakeVenueClient{name: "kalshi", markets: []venue.FetchedMarket{{
		RawJSON: `{}`,
		Canonical: normalize.Market{
			ID: "kalshi:1", Venue: "kalshi", VenueMarketID: "1", Title: "Iffy A", ResolutionDate: base,
			YesPrice: 0.5, NoPrice: 0.5, Liquidity: 100, FetchedAt: base,
		},
	}}}
	polymarket := fakeVenueClient{name: "polymarket", markets: []venue.FetchedMarket{{
		RawJSON: `{}`,
		Canonical: normalize.Market{
			ID: "polymarket:1", Venue: "polymarket", VenueMarketID: "1", Title: "Iffy B", ResolutionDate: base,
			YesPrice: 0.5, NoPrice: 0.5, Liquidity: 100, FetchedAt: base,
		},
	}}}

	st := &fakeRunStore{}
	var out bytes.Buffer

	err := Run(context.Background(), RunDeps{
		Venues: []venue.VenueClient{kalshi, polymarket},
		Store:  st,
		Embedder: fakeMatchEmbedder{vectors: map[string][]float64{
			"Iffy A": {1, 0},
			"Iffy B": {0.75, 0.6614378277661477}, // unit vector, cosine 0.75 vs {1,0}
		}},
		Extractor:  fakeMatchEntityExtractor{},
		DateWindow: 48 * time.Hour,
		Side:       "yes",
		Size:       10,
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "no high-confidence matches found; 1 group(s) need manual review") {
		t.Errorf("expected the needs-review message, got %q", out.String())
	}
	if len(st.insertedRouting) != 0 {
		t.Errorf("expected no routing decision for a needs-review-only result, got %d", len(st.insertedRouting))
	}
}

func TestRunFetchFailurePropagatesAndSkipsMatchAndRoute(t *testing.T) {
	st := &fakeRunStore{}
	var out bytes.Buffer

	err := Run(context.Background(), RunDeps{
		Venues:     []venue.VenueClient{fakeVenueClient{name: "kalshi", err: errors.New("connection refused")}},
		Store:      st,
		Embedder:   fakeMatchEmbedder{},
		DateWindow: 48 * time.Hour,
		Side:       "yes",
		Size:       10,
		Out:        &out,
	})
	if err == nil {
		t.Fatal("expected an error when the only venue fails, got nil")
	}
	if len(st.matchDecisions) != 0 || len(st.insertedRouting) != 0 {
		t.Error("expected match and route to never run after a total fetch failure")
	}
}
