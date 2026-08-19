package venue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const samplePolymarketResponse = `[
  {
    "id": "559651",
    "question": "Xi Jinping out before 2027?",
    "description": "Resolves YES if Xi Jinping is removed from power.",
    "endDate": "2026-12-31T00:00:00Z",
    "outcomes": "[\"Yes\", \"No\"]",
    "outcomePrices": "[\"0.0425\", \"0.9575\"]",
    "liquidityNum": 245300.09627,
    "active": true,
    "closed": false
  },
  {
    "id": "999001",
    "question": "Who will win the election?",
    "description": "Multi-candidate market.",
    "endDate": "2026-11-03T00:00:00Z",
    "outcomes": "[\"Candidate A\", \"Candidate B\", \"Candidate C\"]",
    "outcomePrices": "[\"0.5\", \"0.3\", \"0.2\"]",
    "liquidityNum": 10000,
    "active": true,
    "closed": false
  },
  {
    "id": "999002",
    "question": "Malformed price market",
    "description": "Has an unparseable price.",
    "endDate": "2026-06-01T00:00:00Z",
    "outcomes": "[\"Yes\", \"No\"]",
    "outcomePrices": "[\"not-a-number\", \"0.5\"]",
    "liquidityNum": 500,
    "active": true,
    "closed": false
  }
]`

func TestPolymarketFetchMarkets(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(samplePolymarketResponse))
	}))
	defer server.Close()

	client := NewPolymarketClient(server.URL, nil)
	if client.Name() != "polymarket" {
		t.Errorf("Name() = %q, want polymarket", client.Name())
	}

	markets, err := client.FetchMarkets(context.Background())
	if err != nil {
		t.Fatalf("FetchMarkets: %v", err)
	}

	if gotPath != "/markets?active=true&closed=false&limit=100" {
		t.Errorf("unexpected request path/query: %s", gotPath)
	}

	// Only the first market is valid binary yes/no data; the multi-outcome
	// and malformed-price markets should be skipped, not error the fetch.
	if len(markets) != 1 {
		t.Fatalf("expected 1 valid market, got %d", len(markets))
	}

	got := markets[0].Canonical
	if got.ID != "polymarket:559651" {
		t.Errorf("ID = %q, want polymarket:559651", got.ID)
	}
	if got.Venue != "polymarket" || got.VenueMarketID != "559651" {
		t.Errorf("unexpected venue fields: %+v", got)
	}
	if got.Title != "Xi Jinping out before 2027?" {
		t.Errorf("unexpected title: %q", got.Title)
	}
	if got.YesPrice != 0.0425 || got.NoPrice != 0.9575 {
		t.Errorf("unexpected prices: yes=%v no=%v", got.YesPrice, got.NoPrice)
	}
	if got.Liquidity != 245300.09627 {
		t.Errorf("unexpected liquidity: %v", got.Liquidity)
	}
	if got.ResolutionDate.Year() != 2026 || got.ResolutionDate.Month() != 12 {
		t.Errorf("unexpected resolution date: %v", got.ResolutionDate)
	}

	// RawJSON must be exactly the original per-market JSON, not a re-marshal.
	var roundTrip map[string]any
	if err := json.Unmarshal([]byte(markets[0].RawJSON), &roundTrip); err != nil {
		t.Fatalf("RawJSON is not valid JSON: %v", err)
	}
	if roundTrip["id"] != "559651" {
		t.Errorf("RawJSON did not preserve original market: %v", roundTrip)
	}
}

func TestPolymarketFetchMarketsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewPolymarketClient(server.URL, nil)
	if _, err := client.FetchMarkets(context.Background()); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestPolymarketNormalizeAllOutcomesNonBinary(t *testing.T) {
	pm := polymarketMarket{
		ID: "1", Question: "q", EndDate: "2026-01-01T00:00:00Z",
		Outcomes: `["Maybe"]`, OutcomePrices: `["0.5"]`, LiquidityNum: 100,
	}
	if _, err := pm.normalize(time.Now().UTC()); err == nil {
		t.Fatal("expected error for non-binary outcomes, got nil")
	}
}

func TestPolymarketNormalizeBadDate(t *testing.T) {
	pm := polymarketMarket{
		ID: "1", Question: "q", EndDate: "not-a-date",
		Outcomes: `["Yes", "No"]`, OutcomePrices: `["0.5", "0.5"]`, LiquidityNum: 100,
	}
	if _, err := pm.normalize(time.Now().UTC()); err == nil {
		t.Fatal("expected error for unparseable endDate, got nil")
	}
}
