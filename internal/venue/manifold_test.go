package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sampleManifoldResponse(futureCloseMillis int64) string {
	return fmt.Sprintf(`[
  {
    "id": "ycS6qEQqS8",
    "question": "Will Alan Guth ever win a Nobel Prize?",
    "closeTime": %d,
    "probability": 0.19717336593913337,
    "totalLiquidity": 100,
    "outcomeType": "BINARY",
    "isResolved": false
  },
  {
    "id": "resolved1",
    "question": "Already resolved market",
    "closeTime": %d,
    "probability": 0.9,
    "totalLiquidity": 50,
    "outcomeType": "BINARY",
    "isResolved": true
  },
  {
    "id": "multi1",
    "question": "Multiple choice market",
    "closeTime": %d,
    "probability": 0.5,
    "totalLiquidity": 50,
    "outcomeType": "MULTIPLE_CHOICE",
    "isResolved": false
  }
]`, futureCloseMillis, futureCloseMillis, futureCloseMillis)
}

func TestManifoldFetchMarkets(t *testing.T) {
	futureClose := time.Now().Add(30 * 24 * time.Hour).UnixMilli()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(sampleManifoldResponse(futureClose)))
	}))
	defer server.Close()

	client := NewManifoldClient(server.URL, nil)
	if client.Name() != "manifold" {
		t.Errorf("Name() = %q, want manifold", client.Name())
	}

	markets, err := client.FetchMarkets(context.Background())
	if err != nil {
		t.Fatalf("FetchMarkets: %v", err)
	}

	if gotPath != "/v0/markets?limit=500" {
		t.Errorf("unexpected request path/query: %s", gotPath)
	}

	// The resolved market and the multiple-choice market should be
	// skipped; only the open binary market should survive.
	if len(markets) != 1 {
		t.Fatalf("expected 1 valid market, got %d", len(markets))
	}

	got := markets[0].Canonical
	if got.ID != "manifold:ycS6qEQqS8" {
		t.Errorf("ID = %q, want manifold:ycS6qEQqS8", got.ID)
	}
	if got.Venue != "manifold" || got.VenueMarketID != "ycS6qEQqS8" {
		t.Errorf("unexpected venue fields: %+v", got)
	}
	if got.Title != "Will Alan Guth ever win a Nobel Prize?" {
		t.Errorf("unexpected title: %q", got.Title)
	}
	if got.YesPrice != 0.19717336593913337 {
		t.Errorf("unexpected yes price: %v", got.YesPrice)
	}
	wantNo := 1 - 0.19717336593913337
	if got.NoPrice != wantNo {
		t.Errorf("unexpected no price: %v, want %v", got.NoPrice, wantNo)
	}
	if got.Liquidity != 100 {
		t.Errorf("unexpected liquidity: %v", got.Liquidity)
	}

	var roundTrip map[string]any
	if err := json.Unmarshal([]byte(markets[0].RawJSON), &roundTrip); err != nil {
		t.Fatalf("RawJSON is not valid JSON: %v", err)
	}
	if roundTrip["id"] != "ycS6qEQqS8" {
		t.Errorf("RawJSON did not preserve original market: %v", roundTrip)
	}
}

func TestManifoldFetchMarketsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewManifoldClient(server.URL, nil)
	if _, err := client.FetchMarkets(context.Background()); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestManifoldNormalizeAlreadyClosed(t *testing.T) {
	mm := manifoldMarket{
		ID: "1", Question: "q", OutcomeType: "BINARY", IsResolved: false,
		CloseTime: time.Now().Add(-time.Hour).UnixMilli(), Probability: 0.5, TotalLiquidity: 10,
	}
	if _, err := mm.normalize(time.Now()); err == nil {
		t.Fatal("expected error for a market whose closeTime has passed, got nil")
	}
}
