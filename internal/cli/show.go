package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"equinox/internal/match"
	"equinox/internal/route"
	"equinox/internal/store"
)

// ShowMarketsStore is the subset of *store.Store `show markets` needs.
type ShowMarketsStore interface {
	ListCanonicalMarkets(ctx context.Context, venue string) ([]store.CanonicalMarket, error)
}

// ShowMatchesStore is the subset of *store.Store `show matches` needs.
type ShowMatchesStore interface {
	ListLatestMatchDecisions(ctx context.Context) ([]store.MatchDecision, error)
	LatestMatchDecision(ctx context.Context, eventID string) (store.MatchDecision, error)
}

// ShowDecisionsStore is the subset of *store.Store `show decisions` needs.
type ShowDecisionsStore interface {
	ListRoutingDecisions(ctx context.Context, eventID string) ([]store.RoutingDecision, error)
}

func newTableWriter(out io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ShowMarkets renders canonical_markets — view commands never compute
// anything new, only format rows that already exist (docs/ARCHITECTURE.md).
func ShowMarkets(ctx context.Context, st ShowMarketsStore, out io.Writer, venueFilter string, jsonOutput bool) error {
	rows, err := st.ListCanonicalMarkets(ctx, venueFilter)
	if err != nil {
		return fmt.Errorf("loading markets: %w", err)
	}
	if len(rows) == 0 {
		if venueFilter == "" {
			fmt.Fprintln(out, "no data yet — run `equinox fetch`")
		} else {
			fmt.Fprintf(out, "no markets found for venue %q — run `equinox fetch` if you haven't yet\n", venueFilter)
		}
		return nil
	}

	if jsonOutput {
		views := make([]marketView, len(rows))
		for i, r := range rows {
			views[i] = toMarketView(r)
		}
		return writeJSON(out, views)
	}

	tw := newTableWriter(out)
	fmt.Fprintln(tw, "venue\ttitle\tcategory\tresolution_date\tyes\tno\tliquidity")
	for _, r := range rows {
		category := r.Category
		if category == "" {
			category = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%.2f\t%.2f\t%.2f\n",
			r.Venue, r.Title, category, r.ResolutionDate.Format(time.RFC3339), r.YesPrice, r.NoPrice, r.Liquidity)
	}
	return tw.Flush()
}

// ShowMatches renders match_decisions. With no filter, it shows the
// current best-known group per event (docs/ARCHITECTURE.md's "what do we
// currently believe" view); --event shows just that event's latest
// decision, with its full signal breakdown and members.
func ShowMatches(ctx context.Context, st ShowMatchesStore, out io.Writer, eventFilter string, jsonOutput bool) error {
	if eventFilter != "" {
		d, err := st.LatestMatchDecision(ctx, eventFilter)
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintf(out, "no match found for event %q\n", eventFilter)
			return nil
		}
		if err != nil {
			return fmt.Errorf("loading match decision for %s: %w", eventFilter, err)
		}
		if jsonOutput {
			return writeJSON(out, toMatchView(d))
		}
		return printMatchDetail(out, d)
	}

	rows, err := st.ListLatestMatchDecisions(ctx)
	if err != nil {
		return fmt.Errorf("loading match decisions: %w", err)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "no data yet — run `equinox match` (after `equinox fetch`)")
		return nil
	}

	if jsonOutput {
		views := make([]matchView, len(rows))
		for i, r := range rows {
			views[i] = toMatchView(r)
		}
		return writeJSON(out, views)
	}

	tw := newTableWriter(out)
	fmt.Fprintln(tw, "event\tvenues\tscore\ttier")
	for _, r := range rows {
		venues := make([]string, len(r.Members))
		for i, m := range r.Members {
			venues[i] = m.Venue
		}
		tier := match.ClassifyTier(r.TitleSimilarity, r.DateAlignment)
		fmt.Fprintf(tw, "%s\t%s\t%.2f\t%s\n", r.EventID, strings.Join(venues, ", "), r.Score, tier)
	}
	return tw.Flush()
}

func printMatchDetail(out io.Writer, d store.MatchDecision) error {
	tier := match.ClassifyTier(d.TitleSimilarity, d.DateAlignment)
	fmt.Fprintf(out, "event: %s\n", d.EventID)
	fmt.Fprintf(out, "score: %.2f (tier: %s)\n", d.Score, tier)
	fmt.Fprintf(out, "signals: title_similarity=%.2f  date_alignment=%.2f  category_match=%.2f\n\n",
		d.TitleSimilarity, d.DateAlignment, d.CategoryMatch)

	fmt.Fprintln(out, "members:")
	tw := newTableWriter(out)
	fmt.Fprintln(tw, "venue\tmarket\ttitle")
	for _, m := range d.Members {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", m.Venue, m.CanonicalMarketID, m.Title)
	}
	return tw.Flush()
}

// ShowDecisions renders routing_decisions. Unlike matches, decisions are
// inherently a list even for one event — each is a distinct hypothetical
// order, not a re-evaluation of the same fact — so both the filtered and
// unfiltered forms list every matching row (docs/ROUTING.md).
func ShowDecisions(ctx context.Context, st ShowDecisionsStore, out io.Writer, eventFilter string, jsonOutput bool) error {
	rows, err := st.ListRoutingDecisions(ctx, eventFilter)
	if err != nil {
		return fmt.Errorf("loading routing decisions: %w", err)
	}
	if len(rows) == 0 {
		if eventFilter == "" {
			fmt.Fprintln(out, "no data yet — run `equinox route` (after `equinox match`)")
		} else {
			fmt.Fprintf(out, "no routing decisions found for event %q\n", eventFilter)
		}
		return nil
	}

	if jsonOutput {
		views := make([]decisionView, len(rows))
		for i, r := range rows {
			v, err := toDecisionView(r)
			if err != nil {
				return err
			}
			views[i] = v
		}
		return writeJSON(out, views)
	}

	for i, r := range rows {
		if i > 0 {
			fmt.Fprintln(out)
		}
		if err := printDecisionDetail(out, r); err != nil {
			return err
		}
	}
	return nil
}

func printDecisionDetail(out io.Writer, d store.RoutingDecision) error {
	fmt.Fprintf(out, "event: %s\n", d.EventID)
	fmt.Fprintf(out, "side: %s, size: %v\n\n", d.Side, d.Size)

	if d.IsNoop {
		fmt.Fprintln(out, d.Rationale)
		return nil
	}

	var quotes []route.VenueQuote
	if err := json.Unmarshal([]byte(d.ComparisonJSON), &quotes); err != nil {
		return fmt.Errorf("parsing comparison for event %s: %w", d.EventID, err)
	}

	tw := newTableWriter(out)
	fmt.Fprintln(tw, "venue\tprice\tliquidity_ok\tselected")
	for _, q := range quotes {
		note := ""
		switch {
		case q.Selected:
			note = "  <- best price at requested size"
		case !q.LiquidityOK:
			note = fmt.Sprintf("  <- insufficient liquidity at size %v", d.Size)
		}
		fmt.Fprintf(tw, "%s\t%.2f\t%s\t%s%s\n", q.Venue, q.Price, yesNo(q.LiquidityOK), yesNo(q.Selected), note)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, d.Rationale)
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
