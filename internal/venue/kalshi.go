package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"equinox/internal/normalize"
)

// kalshiPageSize bounds a single fetch to one page of open events, matching
// the fetch scoping assumption in docs/ARCHITECTURE.md. 200 is the observed
// max page size the events endpoint accepts — larger values are rejected
// with a "bad request" error.
const kalshiPageSize = 200

// KalshiClient fetches markets from Kalshi's public trading API. Kalshi's
// real authentication (RSA-PSS request signing with a private key) is not
// needed here: the market data this client reads is served without
// authentication, so no API key is used — see docs/DECISIONS.md.
type KalshiClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewKalshiClient builds a client against baseURL (e.g.
// https://api.elections.kalshi.com). A nil httpClient uses http.DefaultClient.
func NewKalshiClient(baseURL string, httpClient *http.Client) *KalshiClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &KalshiClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *KalshiClient) Name() string { return "kalshi" }

// kalshiEventsResponse is the shape of GET /trade-api/v2/events. Querying
// events (rather than markets directly) is deliberate: Kalshi's default
// /markets listing is currently dominated by auto-generated multivariate
// combo contracts, while /events with_nested_markets=true reliably returns
// single-topic events with real category data and nested single-topic
// markets in one call.
type kalshiEventsResponse struct {
	Events []kalshiEvent `json:"events"`
}

type kalshiEvent struct {
	EventTicker string            `json:"event_ticker"`
	Category    string            `json:"category"`
	Markets     []json.RawMessage `json:"markets"`
}

// kalshiMarket is the subset of nested-market fields this client uses.
//
// Liquidity uses YesAskSizeFP (order size available at the yes ask price),
// not the API's liquidity_dollars field: liquidity_dollars was observed to
// be 0 across every market sampled during development (1514/1514), while
// yes_ask_size_fp — "available size at the quoted price," exactly the
// liquidity proxy docs/ROUTING.md calls for — was populated for 99.5% of
// them.
type kalshiMarket struct {
	Ticker        string `json:"ticker"`
	Title         string `json:"title"`
	RulesPrimary  string `json:"rules_primary"`
	MarketType    string `json:"market_type"`
	CloseTime     string `json:"close_time"`
	YesAskDollars string `json:"yes_ask_dollars"`
	NoAskDollars  string `json:"no_ask_dollars"`
	YesAskSizeFP  string `json:"yes_ask_size_fp"`
}

func (c *KalshiClient) FetchMarkets(ctx context.Context) ([]FetchedMarket, error) {
	url := fmt.Sprintf("%s/trade-api/v2/events?status=open&limit=%d&with_nested_markets=true", c.baseURL, kalshiPageSize)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building kalshi request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching kalshi events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("kalshi API returned %s: %s", resp.Status, string(body))
	}

	var payload kalshiEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decoding kalshi response: %w", err)
	}

	fetchedAt := time.Now().UTC()
	var markets []FetchedMarket
	for _, ev := range payload.Events {
		for _, raw := range ev.Markets {
			var km kalshiMarket
			if err := json.Unmarshal(raw, &km); err != nil {
				continue // malformed entry; skip rather than fail the whole fetch
			}

			canonical, err := km.normalize(ev.Category, fetchedAt)
			if err != nil {
				continue // e.g. non-binary market, unparseable prices/date; skip
			}

			markets = append(markets, FetchedMarket{RawJSON: string(raw), Canonical: canonical})
		}
	}

	return markets, nil
}

// normalize maps one Kalshi market into the canonical Market model. category
// comes from the parent event, since individual markets don't carry it.
func (km kalshiMarket) normalize(category string, fetchedAt time.Time) (normalize.Market, error) {
	if km.MarketType != "binary" {
		return normalize.Market{}, fmt.Errorf("market %s: not a binary market (type %q)", km.Ticker, km.MarketType)
	}

	yesPrice, err := strconv.ParseFloat(km.YesAskDollars, 64)
	if err != nil {
		return normalize.Market{}, fmt.Errorf("market %s: parsing yes_ask_dollars %q: %w", km.Ticker, km.YesAskDollars, err)
	}
	noPrice, err := strconv.ParseFloat(km.NoAskDollars, 64)
	if err != nil {
		return normalize.Market{}, fmt.Errorf("market %s: parsing no_ask_dollars %q: %w", km.Ticker, km.NoAskDollars, err)
	}
	liquidity, err := strconv.ParseFloat(km.YesAskSizeFP, 64)
	if err != nil {
		return normalize.Market{}, fmt.Errorf("market %s: parsing yes_ask_size_fp %q: %w", km.Ticker, km.YesAskSizeFP, err)
	}
	resolutionDate, err := time.Parse(time.RFC3339, km.CloseTime)
	if err != nil {
		return normalize.Market{}, fmt.Errorf("market %s: parsing close_time %q: %w", km.Ticker, km.CloseTime, err)
	}

	m := normalize.Market{
		ID:             normalize.ID("kalshi", km.Ticker),
		Venue:          "kalshi",
		VenueMarketID:  km.Ticker,
		Title:          km.Title,
		Description:    km.RulesPrimary,
		Category:       category,
		ResolutionDate: resolutionDate,
		YesPrice:       yesPrice,
		NoPrice:        noPrice,
		Liquidity:      liquidity,
		FetchedAt:      fetchedAt,
	}
	if err := m.Validate(); err != nil {
		return normalize.Market{}, err
	}
	return m, nil
}
