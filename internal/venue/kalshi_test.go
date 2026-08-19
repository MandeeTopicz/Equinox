package venue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleKalshiResponse = `{
  "events": [
    {
      "event_ticker": "KXELONMARS-99",
      "category": "World",
      "markets": [
        {
          "ticker": "KXELONMARS-99",
          "title": "Will Elon Musk visit Mars before Aug 1, 2099?",
          "rules_primary": "If Elon Musk visits Mars before Aug 1, 2099, then the market resolves to Yes.",
          "market_type": "binary",
          "close_time": "2099-08-08T15:00:00Z",
          "yes_ask_dollars": "0.1200",
          "no_ask_dollars": "0.8900",
          "yes_ask_size_fp": "1250.5000"
        }
      ]
    },
    {
      "event_ticker": "KXWEIRD-01",
      "category": "Economics",
      "markets": [
        {
          "ticker": "KXWEIRD-01-SCALAR",
          "title": "Scalar settlement market",
          "rules_primary": "Not a yes/no market.",
          "market_type": "scalar",
          "close_time": "2026-06-01T00:00:00Z",
          "yes_ask_dollars": "0.5000",
          "no_ask_dollars": "0.5000",
          "yes_ask_size_fp": "100.0000"
        },
        {
          "ticker": "KXWEIRD-01-BADPRICE",
          "title": "Bad price market",
          "rules_primary": "Has an unparseable price.",
          "market_type": "binary",
          "close_time": "2026-06-01T00:00:00Z",
          "yes_ask_dollars": "not-a-number",
          "no_ask_dollars": "0.5000",
          "yes_ask_size_fp": "100.0000"
        }
      ]
    }
  ]
}`

func TestKalshiFetchMarkets(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(sampleKalshiResponse))
	}))
	defer server.Close()

	client := NewKalshiClient(server.URL, nil)
	if client.Name() != "kalshi" {
		t.Errorf("Name() = %q, want kalshi", client.Name())
	}

	markets, err := client.FetchMarkets(context.Background())
	if err != nil {
		t.Fatalf("FetchMarkets: %v", err)
	}

	if gotPath != "/trade-api/v2/events?status=open&limit=200&with_nested_markets=true" {
		t.Errorf("unexpected request path/query: %s", gotPath)
	}

	// The scalar market and the bad-price market should be skipped; only
	// the Elon Musk market should survive.
	if len(markets) != 1 {
		t.Fatalf("expected 1 valid market, got %d", len(markets))
	}

	got := markets[0].Canonical
	if got.ID != "kalshi:KXELONMARS-99" {
		t.Errorf("ID = %q, want kalshi:KXELONMARS-99", got.ID)
	}
	if got.Venue != "kalshi" || got.VenueMarketID != "KXELONMARS-99" {
		t.Errorf("unexpected venue fields: %+v", got)
	}
	if got.Category != "World" {
		t.Errorf("Category = %q, want World (from parent event)", got.Category)
	}
	if got.YesPrice != 0.12 || got.NoPrice != 0.89 {
		t.Errorf("unexpected prices: yes=%v no=%v", got.YesPrice, got.NoPrice)
	}
	if got.Liquidity != 1250.5 {
		t.Errorf("unexpected liquidity: %v", got.Liquidity)
	}
	if got.ResolutionDate.Year() != 2099 {
		t.Errorf("unexpected resolution date: %v", got.ResolutionDate)
	}

	var roundTrip map[string]any
	if err := json.Unmarshal([]byte(markets[0].RawJSON), &roundTrip); err != nil {
		t.Fatalf("RawJSON is not valid JSON: %v", err)
	}
	if roundTrip["ticker"] != "KXELONMARS-99" {
		t.Errorf("RawJSON did not preserve original market: %v", roundTrip)
	}
}

func TestKalshiFetchMarketsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewKalshiClient(server.URL, nil)
	if _, err := client.FetchMarkets(context.Background()); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestKalshiFetchMarketsNoAuthHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range []string{"KALSHI-ACCESS-KEY", "KALSHI-ACCESS-SIGNATURE", "KALSHI-ACCESS-TIMESTAMP", "Authorization"} {
			if r.Header.Get(h) != "" {
				t.Errorf("unexpected auth header %s set on request", h)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"events":[]}`))
	}))
	defer server.Close()

	client := NewKalshiClient(server.URL, nil)
	if _, err := client.FetchMarkets(context.Background()); err != nil {
		t.Fatalf("FetchMarkets: %v", err)
	}
}
