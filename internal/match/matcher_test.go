package match

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"equinox/internal/normalize"
)

// fakeEmbedder returns a pre-assigned vector per exact input text. Unknown
// text yields a zero vector (cosine similarity 0 with anything), so an
// unintended pairing shows up as "doesn't qualify" rather than a panic.
type fakeEmbedder struct {
	vectors map[string][]float64
	err     error
}

func (f fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float64, len(texts))
	for i, text := range texts {
		out[i] = f.vectors[text] // nil (zero-value) for unknown text
	}
	return out, nil
}

func TestMatchGroupsThreeVenuesTransitively(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)

	p := normalize.Market{ID: "polymarket:1", Venue: "polymarket", VenueMarketID: "1", Title: "Market P", Category: "", ResolutionDate: base}
	k := normalize.Market{ID: "kalshi:1", Venue: "kalshi", VenueMarketID: "1", Title: "Market K", Category: "econ", ResolutionDate: base}
	m := normalize.Market{ID: "manifold:1", Venue: "manifold", VenueMarketID: "1", Title: "Market M", Category: "", ResolutionDate: base}

	// Unrelated pair, far outside every other market's date window, forming
	// its own separate group with a perfect score — used to test sort order.
	farDate := base.Add(100 * DefaultDateWindow)
	d := normalize.Market{ID: "polymarket:2", Venue: "polymarket", VenueMarketID: "2", Title: "Market D", Category: "", ResolutionDate: farDate}
	e := normalize.Market{ID: "kalshi:2", Venue: "kalshi", VenueMarketID: "2", Title: "Market E", Category: "econ", ResolutionDate: farDate}

	// P and M are orthogonal (sim=0); K sits at 45 degrees from both
	// (sim=1/sqrt(2) ≈ 0.7071 to each) — see score.go's Composite formula
	// for how this becomes ~0.7932 for P-K and K-M once date alignment is
	// folded in, comfortably above a 0.75 threshold, while P-M's direct
	// pairing (~0.294) stays well below it. P and M still end up in the
	// same group transitively, via K.
	embedder := fakeEmbedder{vectors: map[string][]float64{
		"Market P": {1, 0},
		"Market K": {1, 1},
		"Market M": {0, 1},
		"Market D": {1, 0},
		"Market E": {1, 0}, // identical to D -> sim 1.0
	}}

	groups, _, err := Match(context.Background(), []normalize.Market{p, k, m, d, e}, embedder, fakeEntityExtractor{}, 0.75, DefaultDateWindow)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(groups), groups)
	}

	// Sorted score descending: the D/E group (score 1.0) first.
	if len(groups[0].Members) != 2 {
		t.Errorf("expected the first group to have 2 members (D, E), got %d", len(groups[0].Members))
	}
	if math.Abs(groups[0].Score-1.0) > 1e-9 {
		t.Errorf("first group score = %v, want ~1.0", groups[0].Score)
	}

	second := groups[1]
	if len(second.Members) != 3 {
		t.Fatalf("expected the second group to have 3 members (P, K, M), got %d: %+v", len(second.Members), second.Members)
	}
	if len(second.Pairs) != 2 {
		t.Errorf("expected 2 qualifying edges (P-K, K-M; P-M shouldn't qualify directly), got %d", len(second.Pairs))
	}
	if second.Score < 0.75 || second.Score > 0.85 {
		t.Errorf("second group score = %v, want in [0.75, 0.85]", second.Score)
	}
	if groups[0].Score <= groups[1].Score {
		t.Error("expected groups sorted by score descending")
	}
}

func TestMatchNoCandidatesReturnsNilNotError(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	// Same venue only -> no cross-venue candidates at all.
	markets := []normalize.Market{
		{ID: "polymarket:1", Venue: "polymarket", Title: "A", ResolutionDate: base},
		{ID: "polymarket:2", Venue: "polymarket", Title: "B", ResolutionDate: base},
	}

	groups, _, err := Match(context.Background(), markets, fakeEmbedder{}, fakeEntityExtractor{}, 0.75, DefaultDateWindow)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if groups != nil {
		t.Errorf("expected nil groups, got %+v", groups)
	}
}

func TestMatchThresholdExcludesWeakPairs(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	a := normalize.Market{ID: "polymarket:1", Venue: "polymarket", Title: "A", ResolutionDate: base}
	b := normalize.Market{ID: "kalshi:1", Venue: "kalshi", Title: "B", ResolutionDate: base}

	// Orthogonal vectors -> title similarity 0 -> composite well below any
	// reasonable threshold even with perfect date alignment.
	embedder := fakeEmbedder{vectors: map[string][]float64{"A": {1, 0}, "B": {0, 1}}}

	groups, _, err := Match(context.Background(), []normalize.Market{a, b}, embedder, fakeEntityExtractor{}, 0.75, DefaultDateWindow)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if groups != nil {
		t.Errorf("expected no groups below threshold, got %+v", groups)
	}
}

func TestMatchPropagatesEmbedderError(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	a := normalize.Market{ID: "polymarket:1", Venue: "polymarket", Title: "A", ResolutionDate: base}
	b := normalize.Market{ID: "kalshi:1", Venue: "kalshi", Title: "B", ResolutionDate: base}

	embedder := fakeEmbedder{err: errors.New("api unavailable")}

	if _, _, err := Match(context.Background(), []normalize.Market{a, b}, embedder, fakeEntityExtractor{}, 0.75, DefaultDateWindow); err == nil {
		t.Fatal("expected an error when the embedder fails, got nil")
	}
}

func TestBuildGroupsAggregatesUseMinimumAcrossQualifyingEdges(t *testing.T) {
	p := normalize.Market{ID: "p", Venue: "polymarket", VenueMarketID: "1"}
	k := normalize.Market{ID: "k", Venue: "kalshi", VenueMarketID: "1"}
	m := normalize.Market{ID: "m", Venue: "manifold", VenueMarketID: "1"}

	uf := newUnionFind()
	uf.union(p.ID, k.ID)
	uf.union(k.ID, m.ID)

	qualifying := map[edge]PairScore{
		edgeKey(p.ID, k.ID): {A: p, B: k, Score: Score{Composite: 0.90, TitleSimilarity: 0.95, DateAlignment: 1.0, CategoryMatch: 1}},
		edgeKey(k.ID, m.ID): {A: k, B: m, Score: Score{Composite: 0.80, TitleSimilarity: 0.85, DateAlignment: 0.7, CategoryMatch: 0}},
	}

	groups := buildGroups(uf, qualifying)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	g := groups[0]
	if len(g.Members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(g.Members))
	}
	if g.Score != 0.80 {
		t.Errorf("Score = %v, want 0.80 (min of 0.90, 0.80)", g.Score)
	}
	if g.TitleSimilarity != 0.85 {
		t.Errorf("TitleSimilarity = %v, want 0.85 (min)", g.TitleSimilarity)
	}
	if g.DateAlignment != 0.7 {
		t.Errorf("DateAlignment = %v, want 0.7 (min)", g.DateAlignment)
	}
	if g.CategoryMatch != 0 {
		t.Errorf("CategoryMatch = %v, want 0 (min)", g.CategoryMatch)
	}
	if len(g.Pairs) != 2 {
		t.Errorf("expected 2 qualifying pairs in the group, got %d", len(g.Pairs))
	}
}

func TestEmbeddingText(t *testing.T) {
	withDesc := normalize.Market{Title: "T", Description: "D"}
	if got, want := embeddingText(withDesc), "T\n\nD"; got != want {
		t.Errorf("embeddingText with description = %q, want %q", got, want)
	}

	titleOnly := normalize.Market{Title: "T", Description: ""}
	if got, want := embeddingText(titleOnly), "T"; got != want {
		t.Errorf("embeddingText without description = %q, want %q", got, want)
	}
}
