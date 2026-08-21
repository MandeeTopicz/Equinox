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
	var gotPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path+"?"+r.URL.RawQuery)
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

	// One request is the general paginated fetch (no series_ticker); one
	// is the priority-series fetch for KXFED.
	wantGeneral := "/trade-api/v2/events?limit=200&status=open&with_nested_markets=true"
	wantSeries := "/trade-api/v2/events?limit=200&series_ticker=KXFED&status=open&with_nested_markets=true"
	if len(gotPaths) != 2 || gotPaths[0] != wantGeneral || gotPaths[1] != wantSeries {
		t.Errorf("unexpected requests: %v (want [%q, %q])", gotPaths, wantGeneral, wantSeries)
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

func TestKalshiFetchMarketsFollowsCursorAcrossPages(t *testing.T) {
	page1 := `{
	  "cursor": "page2token",
	  "events": [{
	    "event_ticker": "KXPAGE1",
	    "category": "World",
	    "markets": [{
	      "ticker": "KXPAGE1-A", "title": "Page one market", "rules_primary": "r",
	      "market_type": "binary", "close_time": "2099-08-08T15:00:00Z",
	      "yes_ask_dollars": "0.10", "no_ask_dollars": "0.90", "yes_ask_size_fp": "100"
	    }]
	  }]
	}`
	page2 := `{
	  "cursor": "",
	  "events": [{
	    "event_ticker": "KXPAGE2",
	    "category": "World",
	    "markets": [{
	      "ticker": "KXPAGE2-A", "title": "Page two market", "rules_primary": "r",
	      "market_type": "binary", "close_time": "2099-08-08T15:00:00Z",
	      "yes_ask_dollars": "0.20", "no_ask_dollars": "0.80", "yes_ask_size_fp": "100"
	    }]
	  }]
	}`

	var paginationCursors []string // cursor values for requests with no series_ticker
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("series_ticker") == "" {
			paginationCursors = append(paginationCursors, r.URL.Query().Get("cursor"))
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "" {
			w.Write([]byte(page1))
		} else {
			w.Write([]byte(page2))
		}
	}))
	defer server.Close()

	client := NewKalshiClient(server.URL, nil)
	markets, err := client.FetchMarkets(context.Background())
	if err != nil {
		t.Fatalf("FetchMarkets: %v", err)
	}

	// Both pages' markets plus the priority-series fetch (routed to page1
	// by the mock's cursor=="" rule, but deduped against the page1 result
	// already captured) — still exactly 2 distinct markets.
	if len(markets) != 2 {
		t.Fatalf("expected markets from both pages, got %d: %+v", len(markets), markets)
	}
	if len(paginationCursors) != 2 || paginationCursors[0] != "" || paginationCursors[1] != "page2token" {
		t.Errorf("expected pagination requests with cursors [\"\", \"page2token\"], got %v", paginationCursors)
	}
}

func TestKalshiFetchMarketsStopsAtMaxPages(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		// Always returns a cursor, as if there were infinite pages.
		w.Write([]byte(`{"cursor": "always-more", "events": []}`))
	}))
	defer server.Close()

	client := NewKalshiClient(server.URL, nil)
	if _, err := client.FetchMarkets(context.Background()); err != nil {
		t.Fatalf("FetchMarkets: %v", err)
	}

	// kalshiMaxPages bounded pagination requests, plus one request per
	// priority series (each of those also returns a cursor here, but a
	// priority-series fetch is a single request, not paginated further).
	want := kalshiMaxPages + len(kalshiPrioritySeries)
	if requestCount != want {
		t.Errorf("expected exactly %d requests (bounded pagination + priority series), got %d", want, requestCount)
	}
}

func TestKalshiFetchMarketsIncludesPrioritySeriesBeyondGeneralPages(t *testing.T) {
	general := `{
	  "cursor": "",
	  "events": [{
	    "event_ticker": "KXGENERAL",
	    "category": "World",
	    "markets": [{
	      "ticker": "KXGENERAL-A", "title": "General market", "rules_primary": "r",
	      "market_type": "binary", "close_time": "2099-08-08T15:00:00Z",
	      "yes_ask_dollars": "0.10", "no_ask_dollars": "0.90", "yes_ask_size_fp": "100"
	    }]
	  }]
	}`
	// The KXFED priority series response — a market never seen on the
	// general page, standing in for KXFED-26SEP, which sits far beyond
	// what bounded pagination alone reaches (see kalshi.go's doc comment).
	fedSeries := `{
	  "cursor": "",
	  "events": [{
	    "event_ticker": "KXFED-26SEP",
	    "category": "Economics",
	    "markets": [{
	      "ticker": "KXFED-26SEP-A", "title": "Fed decision market", "rules_primary": "r",
	      "market_type": "binary", "close_time": "2099-08-08T15:00:00Z",
	      "yes_ask_dollars": "0.30", "no_ask_dollars": "0.70", "yes_ask_size_fp": "100"
	    }]
	  }]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("series_ticker") == "KXFED" {
			w.Write([]byte(fedSeries))
		} else {
			w.Write([]byte(general))
		}
	}))
	defer server.Close()

	client := NewKalshiClient(server.URL, nil)
	markets, err := client.FetchMarkets(context.Background())
	if err != nil {
		t.Fatalf("FetchMarkets: %v", err)
	}

	ids := map[string]bool{}
	for _, m := range markets {
		ids[m.Canonical.ID] = true
	}
	if !ids["kalshi:KXGENERAL-A"] {
		t.Error("expected the general-page market to be present")
	}
	if !ids["kalshi:KXFED-26SEP-A"] {
		t.Error("expected the priority-series market (beyond general pages) to be present")
	}
	if len(markets) != 2 {
		t.Errorf("expected exactly 2 distinct markets, got %d: %+v", len(markets), markets)
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
