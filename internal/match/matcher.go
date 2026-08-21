package match

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"equinox/internal/normalize"
)

// PairScore is one candidate pair's composite score and the two markets it
// compares.
type PairScore struct {
	A, B  normalize.Market
	Score Score
}

// Group is a set of canonical markets, from more than one venue, judged to
// represent the same real-world event. The aggregate fields are the
// minimum across the group's qualifying pairwise edges — a group is only
// as confident as its weakest link, consistent with EQUIVALENCE.md's
// precision-over-recall design principle. Tier is derived from the
// (already minimum-aggregated) TitleSimilarity/DateAlignment via
// ClassifyTier — a group is exactly as conservative as applying the tier
// floors to its weakest signals would suggest.
type Group struct {
	Members         []normalize.Market
	Score           float64
	TitleSimilarity float64
	DateAlignment   float64
	CategoryMatch   float64
	Tier            Tier
	Pairs           []PairScore
}

// Trace records what happened to one prefilter survivor: gate-rejected
// (Reason set, Tier/Score zero — scoring never ran), scored but TierNone
// (didn't clear even the review floors), or qualified at some tier.
// Populated for every candidate pair regardless of whether verbose output
// is requested; the CLI's --verbose flag decides whether to print it. See
// docs/EQUIVALENCE.md's "deterministic gates" and "conjunctive tier
// floors" sections.
type Trace struct {
	A, B   normalize.Market
	Tier   Tier
	Reason string
	Score  float64
}

// Match runs the full equivalence detection pipeline over markets
// (docs/EQUIVALENCE.md): a cross-venue heuristic prefilter, deterministic
// gates (numeric threshold, named entities) that can reject a pair outright
// before any scoring, then composite scoring — via embedder for the
// title-similarity signal — for pairs that survive both. A pair qualifies
// only if its title similarity and date alignment independently clear a
// tier's floors (ClassifyTier) — neither can compensate for the other.
// Qualifying pairs (TierMatched or TierNeedsReview) are grouped into
// connected components (so a 3-way match forms one group, not three
// separate pairs — see docs/DECISIONS.md on why a third venue was added).
// Groups are returned sorted by score descending, alongside a trace of
// every candidate pair considered.
func Match(ctx context.Context, markets []normalize.Market, embedder Embedder, extractor EntityExtractor, dateWindow time.Duration) ([]Group, []Trace, error) {
	candidates := candidatePairs(markets, dateWindow)
	if len(candidates) == 0 {
		return nil, nil, nil
	}

	texts, textIndex := uniqueEmbeddingTexts(candidates)
	embeddings, err := embedder.Embed(ctx, texts)
	if err != nil {
		return nil, nil, fmt.Errorf("embedding candidate markets: %w", err)
	}
	if len(embeddings) != len(texts) {
		return nil, nil, fmt.Errorf("embedder returned %d vectors for %d inputs", len(embeddings), len(texts))
	}

	entitiesByTitle, err := extractAllEntities(ctx, candidates, extractor)
	if err != nil {
		return nil, nil, fmt.Errorf("extracting entities from candidate markets: %w", err)
	}

	uf := newUnionFind()
	qualifying := map[edge]PairScore{}
	traces := make([]Trace, 0, len(candidates))
	for _, c := range candidates {
		if gate := ThresholdGate(c.a, c.b); !gate.Passed {
			traces = append(traces, Trace{A: c.a, B: c.b, Reason: gate.Reason})
			continue
		}
		if gate := entityGateFromSets(entitiesByTitle[c.a.Title], entitiesByTitle[c.b.Title]); !gate.Passed {
			traces = append(traces, Trace{A: c.a, B: c.b, Reason: gate.Reason})
			continue
		}

		titleSim := cosineSimilarity(embeddings[textIndex[c.a.ID]], embeddings[textIndex[c.b.ID]])
		score := Composite(c.a, c.b, titleSim, dateWindow)
		tier := ClassifyTier(score.TitleSimilarity, score.DateAlignment)
		traces = append(traces, Trace{A: c.a, B: c.b, Tier: tier, Score: score.Composite})
		if tier == TierNone {
			continue
		}
		uf.union(c.a.ID, c.b.ID)
		qualifying[edgeKey(c.a.ID, c.b.ID)] = PairScore{A: c.a, B: c.b, Score: score}
	}
	if len(qualifying) == 0 {
		return nil, traces, nil
	}

	groups := buildGroups(uf, qualifying)
	sort.Slice(groups, func(i, j int) bool { return groups[i].Score > groups[j].Score })
	return groups, traces, nil
}

// extractAllEntities extracts entities for each unique candidate market
// title exactly once, however many pairs it appears in.
func extractAllEntities(ctx context.Context, candidates []candidatePair, extractor EntityExtractor) (map[string][]string, error) {
	result := map[string][]string{}
	extract := func(title string) error {
		if _, ok := result[title]; ok {
			return nil
		}
		entities, err := extractor.ExtractEntities(ctx, title)
		if err != nil {
			return fmt.Errorf("title %q: %w", title, err)
		}
		result[title] = entities
		return nil
	}
	for _, c := range candidates {
		if err := extract(c.a.Title); err != nil {
			return nil, err
		}
		if err := extract(c.b.Title); err != nil {
			return nil, err
		}
	}
	return result, nil
}

type candidatePair struct{ a, b normalize.Market }

// candidatePairs generates every cross-venue pair that clears the
// prefilter, grouping markets by venue first so same-venue pairs are never
// even generated (Prefilter would reject them anyway, but this avoids
// O(n^2) waste across a single venue's own listings).
func candidatePairs(markets []normalize.Market, dateWindow time.Duration) []candidatePair {
	byVenue := map[string][]normalize.Market{}
	var venues []string
	for _, m := range markets {
		if _, ok := byVenue[m.Venue]; !ok {
			venues = append(venues, m.Venue)
		}
		byVenue[m.Venue] = append(byVenue[m.Venue], m)
	}
	sort.Strings(venues)

	var candidates []candidatePair
	for i := 0; i < len(venues); i++ {
		for j := i + 1; j < len(venues); j++ {
			for _, a := range byVenue[venues[i]] {
				for _, b := range byVenue[venues[j]] {
					if Prefilter(a, b, dateWindow).Passed {
						candidates = append(candidates, candidatePair{a, b})
					}
				}
			}
		}
	}
	return candidates
}

// uniqueEmbeddingTexts collects each candidate market's embedding text
// exactly once, however many pairs it appears in, so a market involved in
// many candidate pairs is only ever embedded once.
func uniqueEmbeddingTexts(candidates []candidatePair) ([]string, map[string]int) {
	texts := make([]string, 0, len(candidates)*2)
	index := map[string]int{}
	add := func(m normalize.Market) {
		if _, ok := index[m.ID]; ok {
			return
		}
		index[m.ID] = len(texts)
		texts = append(texts, embeddingText(m))
	}
	for _, c := range candidates {
		add(c.a)
		add(c.b)
	}
	return texts, index
}

// embeddingText is the text embedded for semantic similarity: title alone
// when a venue provides no description (Manifold — see internal/venue),
// otherwise title and description together.
func embeddingText(m normalize.Market) string {
	if m.Description == "" {
		return m.Title
	}
	return m.Title + "\n\n" + m.Description
}

// buildGroups turns qualifying edges into Groups, one per connected
// component in uf.
func buildGroups(uf *unionFind, qualifying map[edge]PairScore) []Group {
	membersByRoot := map[string]map[string]normalize.Market{}
	pairsByRoot := map[string][]PairScore{}

	for e, ps := range qualifying {
		root := uf.find(e[0])
		if membersByRoot[root] == nil {
			membersByRoot[root] = map[string]normalize.Market{}
		}
		membersByRoot[root][ps.A.ID] = ps.A
		membersByRoot[root][ps.B.ID] = ps.B
		pairsByRoot[root] = append(pairsByRoot[root], ps)
	}

	groups := make([]Group, 0, len(membersByRoot))
	for root, memberSet := range membersByRoot {
		members := make([]normalize.Market, 0, len(memberSet))
		for _, m := range memberSet {
			members = append(members, m)
		}
		sort.Slice(members, func(i, j int) bool {
			if members[i].Venue != members[j].Venue {
				return members[i].Venue < members[j].Venue
			}
			return members[i].VenueMarketID < members[j].VenueMarketID
		})

		pairs := pairsByRoot[root]
		minScore, minTitle, minDate, minCategory := math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1)
		for _, ps := range pairs {
			minScore = math.Min(minScore, ps.Score.Composite)
			minTitle = math.Min(minTitle, ps.Score.TitleSimilarity)
			minDate = math.Min(minDate, ps.Score.DateAlignment)
			minCategory = math.Min(minCategory, ps.Score.CategoryMatch)
		}

		groups = append(groups, Group{
			Members:         members,
			Score:           minScore,
			TitleSimilarity: minTitle,
			DateAlignment:   minDate,
			CategoryMatch:   minCategory,
			Tier:            ClassifyTier(minTitle, minDate),
			Pairs:           pairs,
		})
	}
	return groups
}

// edge is an order-independent pair key.
type edge [2]string

func edgeKey(a, b string) edge {
	if a < b {
		return edge{a, b}
	}
	return edge{b, a}
}

// unionFind is a minimal disjoint-set structure keyed by canonical market
// id, used to group transitively-matched pairs into connected components
// (e.g. A-B and B-C qualifying makes {A,B,C} one group).
type unionFind struct {
	parent map[string]string
}

func newUnionFind() *unionFind {
	return &unionFind{parent: map[string]string{}}
}

func (u *unionFind) find(x string) string {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
	}
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x])
	}
	return u.parent[x]
}

func (u *unionFind) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra != rb {
		u.parent[ra] = rb
	}
}
