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
	Extractor  match.EntityExtractor
	DateWindow time.Duration
	Verbose    bool // print a per-candidate-pair trace, including gate rejections
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
	Tier              string  `json:"tier"`
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

	groups, traces, err := match.Match(ctx, markets, deps.Embedder, deps.Extractor, deps.DateWindow)
	if err != nil {
		return fmt.Errorf("matching: %w", err)
	}

	if deps.Verbose {
		printMatchTrace(deps.Out, traces)
	}

	now := time.Now().UTC()
	usedSlugs := map[string]bool{}
	var matchedCount, reviewCount int
	for _, g := range groups {
		if g.Tier == match.TierMatched {
			matchedCount++
		} else {
			reviewCount++
		}

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
				Tier:              match.ClassifyTier(p.Score.TitleSimilarity, p.Score.DateAlignment).String(),
			}
		}
		signalsJSON, err := json.Marshal(breakdown)
		if err != nil {
			return fmt.Errorf("marshaling signal breakdown for %s: %w", eventID, err)
		}

		_, err = deps.Store.InsertMatchDecision(ctx, store.MatchDecision{
			EventID:   eventID,
			CreatedAt: now,
			// MinScore is vestigial: matching no longer gates on a single
			// tunable threshold (see docs/DECISIONS.md on why --min-score
			// was retired in favor of fixed conjunctive tier floors). Kept
			// at the review tier's title floor — the lowest bar any
			// persisted pair had to clear at all — for schema continuity
			// rather than a migration; the actual gate is match.Tier,
			// derived from TitleSimilarity/DateAlignment at read time.
			MinScore:        match.ReviewTitleFloor,
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

	fmt.Fprintf(deps.Out, "matched %d cross-venue groups (%d matched, %d needs review)\n", len(groups), matchedCount, reviewCount)
	return nil
}

// printMatchTrace prints one line per candidate pair for --verbose:
// ephemeral, this run only — not persisted (see docs/EQUIVALENCE.md).
func printMatchTrace(out io.Writer, traces []match.Trace) {
	for _, t := range traces {
		fmt.Fprintf(out, "checked: %s vs %s\n", t.A.ID, t.B.ID)
		switch {
		case t.Reason != "":
			fmt.Fprintf(out, "  rejected: %s\n", t.Reason)
		case t.Tier == match.TierNone:
			fmt.Fprintf(out, "  below review floors: score %.2f\n", t.Score)
		default:
			fmt.Fprintf(out, "  %s: score %.2f\n", t.Tier, t.Score)
		}
	}
}
