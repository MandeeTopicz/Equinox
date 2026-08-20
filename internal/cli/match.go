package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"equinox/internal/match"
	"equinox/internal/normalize"
	"equinox/internal/store"
)

// MatchStore is the subset of *store.Store the match command needs.
type MatchStore interface {
	ListCanonicalMarkets(ctx context.Context, venue string) ([]store.CanonicalMarket, error)
	InsertMatchDecision(ctx context.Context, d store.MatchDecision) (int64, error)
}

// MatchDeps holds match's constructed dependencies.
type MatchDeps struct {
	Store      MatchStore
	Embedder   match.Embedder
	MinScore   float64
	DateWindow time.Duration
	Out        io.Writer
}

// pairBreakdown is one qualifying pairwise edge's full signal breakdown,
// persisted in match_decisions.signals_json — the rationale is the
// breakdown, not just the final score (docs/EQUIVALENCE.md).
type pairBreakdown struct {
	MarketA           string  `json:"market_a"`
	MarketB           string  `json:"market_b"`
	Composite         float64 `json:"composite"`
	TitleSimilarity   float64 `json:"title_similarity"`
	DateAlignment     float64 `json:"date_alignment"`
	CategoryMatch     float64 `json:"category_match"`
	CategoryEvaluated bool    `json:"category_evaluated"`
}

// Match runs equivalence detection over every canonical market currently in
// the store and records each resulting group as a new match_decisions row.
// match_decisions is an event table: running Match again adds new rows
// rather than overwriting prior ones (docs/ARCHITECTURE.md).
func Match(ctx context.Context, deps MatchDeps) error {
	rows, err := deps.Store.ListCanonicalMarkets(ctx, "")
	if err != nil {
		return fmt.Errorf("loading canonical markets: %w", err)
	}
	if len(rows) == 0 {
		fmt.Fprintln(deps.Out, "no canonical markets yet — run `equinox fetch` first")
		return nil
	}

	markets := make([]normalize.Market, 0, len(rows))
	for _, r := range rows {
		markets = append(markets, toNormalizeMarket(r))
	}

	groups, err := match.Match(ctx, markets, deps.Embedder, deps.MinScore, deps.DateWindow)
	if err != nil {
		return fmt.Errorf("matching: %w", err)
	}

	now := time.Now().UTC()
	usedSlugs := map[string]bool{}
	for _, g := range groups {
		eventID := match.UniqueSlug(g.Members[0].Title, usedSlugs)
		usedSlugs[eventID] = true

		members := make([]store.MatchMember, len(g.Members))
		for i, m := range g.Members {
			members[i] = store.MatchMember{Venue: m.Venue, CanonicalMarketID: m.ID, Title: m.Title}
		}

		breakdown := make([]pairBreakdown, len(g.Pairs))
		for i, p := range g.Pairs {
			breakdown[i] = pairBreakdown{
				MarketA: p.A.ID, MarketB: p.B.ID,
				Composite: p.Score.Composite, TitleSimilarity: p.Score.TitleSimilarity,
				DateAlignment: p.Score.DateAlignment, CategoryMatch: p.Score.CategoryMatch,
				CategoryEvaluated: p.Score.CategoryEvaluated,
			}
		}
		signalsJSON, err := json.Marshal(breakdown)
		if err != nil {
			return fmt.Errorf("marshaling signal breakdown for %s: %w", eventID, err)
		}

		_, err = deps.Store.InsertMatchDecision(ctx, store.MatchDecision{
			EventID:         eventID,
			CreatedAt:       now,
			MinScore:        deps.MinScore,
			Score:           g.Score,
			TitleSimilarity: g.TitleSimilarity,
			DateAlignment:   g.DateAlignment,
			CategoryMatch:   g.CategoryMatch,
			Members:         members,
			SignalsJSON:     string(signalsJSON),
		})
		if err != nil {
			return fmt.Errorf("storing match decision for %s: %w", eventID, err)
		}
	}

	fmt.Fprintf(deps.Out, "matched %d cross-venue groups (min-score %.2f)\n", len(groups), deps.MinScore)
	return nil
}
