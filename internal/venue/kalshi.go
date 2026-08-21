package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"equinox/internal/normalize"
)

// kalshiPageSize is the max page size the events endpoint accepts — larger
// values are rejected with a "bad request" error.
const kalshiPageSize = 200

// kalshiMaxPages bounds how many pages of open events the general fetch
// follows. This is still a deliberately bounded fetch, consistent with the
// scoping assumption in docs/ARCHITECTURE.md ("unbounded ingestion...isn't
// tractable for a prototype") — just a wider bound than one page.
const kalshiMaxPages = 5

// kalshiPrioritySeries names series fetched explicitly, on top of the
// bounded general pagination above, via the series_ticker filter.
//
// This exists because Kalshi's /events endpoint has no relevance/activity
// sort or filter — sort, order_by, category, min_volume, and min_liquidity
// were all tried during development and silently ignored, returning the
// same fixed default order every time. That default order is not
// recency- or liquidity-weighted: KXFED-26SEP, a real, live Fed rate
// decision event with a direct Polymarket counterpart (confirmed
// independently — Polymarket's Gamma API, sorted by volume, surfaces
// "Will the Fed decrease interest rates by 25 bps after the September 2026
// meeting?" for the same date), sits at position #5552 in that fixed
// order — page 28, far past what kalshiMaxPages reaches.
//
// KXFED ("Fed funds rate") is included because it's independently verified
// as a real, currently-live topic with a genuine cross-venue counterpart,
// not chosen to guarantee a particular demo outcome. Kept deliberately
// short: the point is disclosed, targeted coverage of a topic known to
// recur across major prediction-market venues, not a curated list built to
// make a specific result appear. See docs/DECISIONS.md.
var kalshiPrioritySeries = []string{"KXFED"}

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
	Cursor string        `json:"cursor"` // empty when there are no more pages
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
	fetchedAt := time.Now().UTC()
	seen := map[string]bool{} // canonical market ID -> already added
	var markets []FetchedMarket

	add := func(events []kalshiEvent) {
		for _, ev := range events {
			for _, raw := range ev.Markets {
				var km kalshiMarket
				if err := json.Unmarshal(raw, &km); err != nil {
					continue // malformed entry; skip rather than fail the whole fetch
				}

				canonical, err := km.normalize(ev.Category, fetchedAt)
				if err != nil {
					continue // e.g. non-binary market, unparseable prices/date; skip
				}

				if seen[canonical.ID] {
					continue // already captured by an earlier page or priority series
				}
				seen[canonical.ID] = true
				markets = append(markets, FetchedMarket{RawJSON: string(raw), Canonical: canonical})
			}
		}
	}

	cursor := ""
	for page := 0; page < kalshiMaxPages; page++ {
		extra := url.Values{}
		if cursor != "" {
			extra.Set("cursor", cursor)
		}
		payload, err := c.fetchEvents(ctx, extra)
		if err != nil {
			return nil, err
		}
		add(payload.Events)

		if payload.Cursor == "" {
			break // no more pages
		}
		cursor = payload.Cursor
	}

	for _, series := range kalshiPrioritySeries {
		payload, err := c.fetchEvents(ctx, url.Values{"series_ticker": {series}})
		if err != nil {
			return nil, fmt.Errorf("fetching priority series %s: %w", series, err)
		}
		add(payload.Events)
	}

	return markets, nil
}

// fetchEvents issues one GET /events request with status=open,
// with_nested_markets=true, and limit already set, plus any caller-supplied
// params (a pagination cursor, or a series_ticker filter).
func (c *KalshiClient) fetchEvents(ctx context.Context, extra url.Values) (kalshiEventsResponse, error) {
	q := url.Values{
		"status":              {"open"},
		"limit":               {strconv.Itoa(kalshiPageSize)},
		"with_nested_markets": {"true"},
	}
	for k, v := range extra {
		q[k] = v
	}
	reqURL := fmt.Sprintf("%s/trade-api/v2/events?%s", c.baseURL, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return kalshiEventsResponse{}, fmt.Errorf("building kalshi request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return kalshiEventsResponse{}, fmt.Errorf("fetching kalshi events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return kalshiEventsResponse{}, fmt.Errorf("kalshi API returned %s: %s", resp.Status, string(body))
	}

	var payload kalshiEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return kalshiEventsResponse{}, fmt.Errorf("decoding kalshi response: %w", err)
	}
	return payload, nil
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
