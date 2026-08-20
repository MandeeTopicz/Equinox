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
// precision-over-recall design principle.
type Group struct {
	Members         []normalize.Market
	Score           float64
	TitleSimilarity float64
	DateAlignment   float64
	CategoryMatch   float64
	Pairs           []PairScore
}

// Match runs the full two-stage equivalence detection over markets
// (docs/EQUIVALENCE.md): a cross-venue heuristic prefilter, then composite
// scoring — via embedder for the title-similarity signal — for pairs that
// survive it. Pairs scoring at or above minScore are grouped into
// connected components (so a 3-way match forms one group, not three
// separate pairs — see docs/DECISIONS.md on why a third venue was added).
// Groups are returned sorted by score descending.
func Match(ctx context.Context, markets []normalize.Market, embedder Embedder, minScore float64, dateWindow time.Duration) ([]Group, error) {
	candidates := candidatePairs(markets, dateWindow)
	if len(candidates) == 0 {
		return nil, nil
	}

	texts, textIndex := uniqueEmbeddingTexts(candidates)
	embeddings, err := embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embedding candidate markets: %w", err)
	}
	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d inputs", len(embeddings), len(texts))
	}

	uf := newUnionFind()
	qualifying := map[edge]PairScore{}
	for _, c := range candidates {
		titleSim := cosineSimilarity(embeddings[textIndex[c.a.ID]], embeddings[textIndex[c.b.ID]])
		score := Composite(c.a, c.b, titleSim, dateWindow)
		if score.Composite < minScore {
			continue
		}
		uf.union(c.a.ID, c.b.ID)
		qualifying[edgeKey(c.a.ID, c.b.ID)] = PairScore{A: c.a, B: c.b, Score: score}
	}
	if len(qualifying) == 0 {
		return nil, nil
	}

	groups := buildGroups(uf, qualifying)
	sort.Slice(groups, func(i, j int) bool { return groups[i].Score > groups[j].Score })
	return groups, nil
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
