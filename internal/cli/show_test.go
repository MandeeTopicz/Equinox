package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"equinox/internal/store"
)

type fakeShowStore struct {
	markets          []store.CanonicalMarket
	latestMatches    []store.MatchDecision
	matchByEvent     map[string]store.MatchDecision
	routingDecisions map[string][]store.RoutingDecision // keyed by event id; "" holds the unfiltered set
}

func (f *fakeShowStore) ListCanonicalMarkets(ctx context.Context, venue string) ([]store.CanonicalMarket, error) {
	if venue == "" {
		return f.markets, nil
	}
	var out []store.CanonicalMarket
	for _, m := range f.markets {
		if m.Venue == venue {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeShowStore) ListLatestMatchDecisions(ctx context.Context) ([]store.MatchDecision, error) {
	return f.latestMatches, nil
}

func (f *fakeShowStore) LatestMatchDecision(ctx context.Context, eventID string) (store.MatchDecision, error) {
	d, ok := f.matchByEvent[eventID]
	if !ok {
		return store.MatchDecision{}, sql.ErrNoRows
	}
	return d, nil
}

func (f *fakeShowStore) ListRoutingDecisions(ctx context.Context, eventID string) ([]store.RoutingDecision, error) {
	return f.routingDecisions[eventID], nil
}

func TestShowMarketsNoDataYet(t *testing.T) {
	var out bytes.Buffer
	if err := ShowMarkets(context.Background(), &fakeShowStore{}, &out, "", false); err != nil {
		t.Fatalf("ShowMarkets: %v", err)
	}
	if !strings.Contains(out.String(), "no data yet") {
		t.Errorf("expected a no-data message, got %q", out.String())
	}
}

func TestShowMarketsNoDataForVenueFilter(t *testing.T) {
	st := &fakeShowStore{markets: []store.CanonicalMarket{{ID: "kalshi:1", Venue: "kalshi"}}}
	var out bytes.Buffer
	if err := ShowMarkets(context.Background(), st, &out, "polymarket", false); err != nil {
		t.Fatalf("ShowMarkets: %v", err)
	}
	if !strings.Contains(out.String(), `no markets found for venue "polymarket"`) {
		t.Errorf("expected a venue-specific no-data message, got %q", out.String())
	}
}

func TestShowMarketsTable(t *testing.T) {
	now := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	st := &fakeShowStore{markets: []store.CanonicalMarket{
		{ID: "kalshi:1", Venue: "kalshi", Title: "Fed rate cut?", Category: "econ", ResolutionDate: now, YesPrice: 0.62, NoPrice: 0.38, Liquidity: 1000},
		{ID: "polymarket:1", Venue: "polymarket", Title: "Fed cut in March?", ResolutionDate: now, YesPrice: 0.65, NoPrice: 0.35, Liquidity: 500},
	}}
	var out bytes.Buffer
	if err := ShowMarkets(context.Background(), st, &out, "", false); err != nil {
		t.Fatalf("ShowMarkets: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "venue") || !strings.Contains(output, "title") {
		t.Errorf("expected a header row, got %q", output)
	}
	if !strings.Contains(output, "kalshi") || !strings.Contains(output, "Fed rate cut?") {
		t.Errorf("expected kalshi row data, got %q", output)
	}
	// Polymarket has no category; should render as "-", not blank.
	if !strings.Contains(output, "polymarket") {
		t.Errorf("expected polymarket row, got %q", output)
	}
}

func TestShowMarketsJSON(t *testing.T) {
	now := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	st := &fakeShowStore{markets: []store.CanonicalMarket{
		{ID: "kalshi:1", Venue: "kalshi", Title: "Fed rate cut?", ResolutionDate: now, YesPrice: 0.62, NoPrice: 0.38, Liquidity: 1000},
	}}
	var out bytes.Buffer
	if err := ShowMarkets(context.Background(), st, &out, "", true); err != nil {
		t.Fatalf("ShowMarkets: %v", err)
	}

	var views []marketView
	if err := json.Unmarshal(out.Bytes(), &views); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(views) != 1 || views[0].ID != "kalshi:1" {
		t.Errorf("unexpected JSON output: %+v", views)
	}
}

func TestShowMatchesNoDataYet(t *testing.T) {
	var out bytes.Buffer
	if err := ShowMatches(context.Background(), &fakeShowStore{}, &out, "", false); err != nil {
		t.Fatalf("ShowMatches: %v", err)
	}
	if !strings.Contains(out.String(), "no data yet") {
		t.Errorf("expected a no-data message, got %q", out.String())
	}
}

func TestShowMatchesTable(t *testing.T) {
	st := &fakeShowStore{latestMatches: []store.MatchDecision{
		{EventID: "fed-march-2026-cut", Score: 0.91, Members: []store.MatchMember{
			{Venue: "polymarket"}, {Venue: "kalshi"}, {Venue: "manifold"},
		}},
	}}
	var out bytes.Buffer
	if err := ShowMatches(context.Background(), st, &out, "", false); err != nil {
		t.Fatalf("ShowMatches: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "fed-march-2026-cut") || !strings.Contains(output, "0.91") {
		t.Errorf("expected the match row, got %q", output)
	}
	if !strings.Contains(output, "polymarket, kalshi, manifold") {
		t.Errorf("expected comma-joined venues, got %q", output)
	}
}

func TestShowMatchesFilteredByEventNotFound(t *testing.T) {
	var out bytes.Buffer
	if err := ShowMatches(context.Background(), &fakeShowStore{}, &out, "no-such-event", false); err != nil {
		t.Fatalf("ShowMatches: %v", err)
	}
	if !strings.Contains(out.String(), `no match found for event "no-such-event"`) {
		t.Errorf("expected a not-found message, got %q", out.String())
	}
}

func TestShowMatchesFilteredByEventDetail(t *testing.T) {
	st := &fakeShowStore{matchByEvent: map[string]store.MatchDecision{
		"fed-march-2026-cut": {
			EventID: "fed-march-2026-cut", Score: 0.91, MinScore: 0.75,
			TitleSimilarity: 0.95, DateAlignment: 1.0, CategoryMatch: 0,
			Members: []store.MatchMember{{Venue: "kalshi", CanonicalMarketID: "kalshi:1", Title: "Fed rate cut?"}},
		},
	}}
	var out bytes.Buffer
	if err := ShowMatches(context.Background(), st, &out, "fed-march-2026-cut", false); err != nil {
		t.Fatalf("ShowMatches: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "event: fed-march-2026-cut") {
		t.Errorf("expected event header, got %q", output)
	}
	if !strings.Contains(output, "title_similarity=0.95") {
		t.Errorf("expected signal breakdown, got %q", output)
	}
	if !strings.Contains(output, "kalshi:1") {
		t.Errorf("expected members table, got %q", output)
	}
}

func TestShowDecisionsNoDataYet(t *testing.T) {
	var out bytes.Buffer
	if err := ShowDecisions(context.Background(), &fakeShowStore{}, &out, "", false); err != nil {
		t.Fatalf("ShowDecisions: %v", err)
	}
	if !strings.Contains(out.String(), "no data yet") {
		t.Errorf("expected a no-data message, got %q", out.String())
	}
}

func TestShowDecisionsNoDataForEventFilter(t *testing.T) {
	var out bytes.Buffer
	if err := ShowDecisions(context.Background(), &fakeShowStore{}, &out, "some-event", false); err != nil {
		t.Fatalf("ShowDecisions: %v", err)
	}
	if !strings.Contains(out.String(), `no routing decisions found for event "some-event"`) {
		t.Errorf("expected an event-specific no-data message, got %q", out.String())
	}
}

func TestShowDecisionsTable(t *testing.T) {
	comparison := `[{"venue":"kalshi","price":0.62,"liquidity":1000,"liquidity_ok":true,"selected":true},` +
		`{"venue":"polymarket","price":0.65,"liquidity":1000,"liquidity_ok":true,"selected":false},` +
		`{"venue":"manifold","price":0.60,"liquidity":50,"liquidity_ok":false,"selected":false}]`

	st := &fakeShowStore{routingDecisions: map[string][]store.RoutingDecision{
		"fed-march-2026-cut": {{
			EventID: "fed-march-2026-cut", Side: "yes", Size: 100,
			SelectedVenue:  sql.NullString{String: "kalshi", Valid: true},
			Rationale:      "selected: kalshi — best YES price at requested size (0.62 vs. polymarket 0.65); manifold excluded on liquidity",
			ComparisonJSON: comparison,
		}},
	}}
	var out bytes.Buffer
	if err := ShowDecisions(context.Background(), st, &out, "fed-march-2026-cut", false); err != nil {
		t.Fatalf("ShowDecisions: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "event: fed-march-2026-cut") {
		t.Errorf("expected event header, got %q", output)
	}
	if !strings.Contains(output, "side: yes, size: 100") {
		t.Errorf("expected side/size line, got %q", output)
	}
	if !strings.Contains(output, "<- best price at requested size") {
		t.Errorf("expected the selected-venue annotation, got %q", output)
	}
	if !strings.Contains(output, "<- insufficient liquidity at size 100") {
		t.Errorf("expected the liquidity-exclusion annotation, got %q", output)
	}
	if !strings.Contains(output, "selected: kalshi") {
		t.Errorf("expected the rationale line, got %q", output)
	}
}

func TestShowDecisionsNoopRendersRationaleOnly(t *testing.T) {
	st := &fakeShowStore{routingDecisions: map[string][]store.RoutingDecision{
		"kalshi:solo": {{
			EventID: "kalshi:solo", Side: "yes", Size: 5, IsNoop: true,
			Rationale: "no match group for kalshi:solo; single-venue no-op — only kalshi available (yes price 0.12)",
		}},
	}}
	var out bytes.Buffer
	if err := ShowDecisions(context.Background(), st, &out, "kalshi:solo", false); err != nil {
		t.Fatalf("ShowDecisions: %v", err)
	}
	if !strings.Contains(out.String(), "single-venue no-op") {
		t.Errorf("expected the no-op rationale, got %q", out.String())
	}
}

func TestShowDecisionsJSON(t *testing.T) {
	comparison := `[{"venue":"kalshi","price":0.62,"liquidity":1000,"liquidity_ok":true,"selected":true}]`
	st := &fakeShowStore{routingDecisions: map[string][]store.RoutingDecision{
		"fed-march-2026-cut": {{
			EventID: "fed-march-2026-cut", Side: "yes", Size: 100,
			SelectedVenue:  sql.NullString{String: "kalshi", Valid: true},
			ComparisonJSON: comparison,
		}},
	}}
	var out bytes.Buffer
	if err := ShowDecisions(context.Background(), st, &out, "fed-march-2026-cut", true); err != nil {
		t.Fatalf("ShowDecisions: %v", err)
	}

	var views []decisionView
	if err := json.Unmarshal(out.Bytes(), &views); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(views) != 1 || views[0].SelectedVenue != "kalshi" || len(views[0].Quotes) != 1 {
		t.Errorf("unexpected JSON output: %+v", views)
	}
}
