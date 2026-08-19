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

// polymarketPageSize matches the Gamma API's observed page cap — requesting
// more does not return more. Ingestion is scoped to one page of active/open
// markets, consistent with the fetch scoping assumption in
// docs/ARCHITECTURE.md; a production system would need pagination to go
// further.
const polymarketPageSize = 100

// PolymarketClient fetches markets from Polymarket's Gamma API. Polymarket
// requires no authentication (see equinox.yaml).
type PolymarketClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewPolymarketClient builds a client against baseURL (e.g.
// https://gamma-api.polymarket.com). A nil httpClient uses http.DefaultClient.
func NewPolymarketClient(baseURL string, httpClient *http.Client) *PolymarketClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &PolymarketClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *PolymarketClient) Name() string { return "polymarket" }

// polymarketMarket is the subset of Gamma API market fields this client
// uses. The API returns many more fields (order book config, reward
// metadata, etc.) that are irrelevant to the canonical model and left
// unparsed.
type polymarketMarket struct {
	ID            string  `json:"id"`
	Question      string  `json:"question"`
	Description   string  `json:"description"`
	EndDate       string  `json:"endDate"`
	Outcomes      string  `json:"outcomes"`      // JSON-encoded array, e.g. "[\"Yes\", \"No\"]"
	OutcomePrices string  `json:"outcomePrices"` // JSON-encoded array, e.g. "[\"0.65\", \"0.35\"]"
	LiquidityNum  float64 `json:"liquidityNum"`
}

func (c *PolymarketClient) FetchMarkets(ctx context.Context) ([]FetchedMarket, error) {
	url := fmt.Sprintf("%s/markets?active=true&closed=false&limit=%d", c.baseURL, polymarketPageSize)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building polymarket request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching polymarket markets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("polymarket API returned %s: %s", resp.Status, string(body))
	}

	var rawMarkets []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawMarkets); err != nil {
		return nil, fmt.Errorf("decoding polymarket response: %w", err)
	}

	fetchedAt := time.Now().UTC()
	markets := make([]FetchedMarket, 0, len(rawMarkets))
	for _, raw := range rawMarkets {
		var pm polymarketMarket
		if err := json.Unmarshal(raw, &pm); err != nil {
			continue // malformed entry; skip rather than fail the whole fetch
		}

		canonical, err := pm.normalize(fetchedAt)
		if err != nil {
			continue // e.g. non-binary market, unparseable prices/date; skip
		}

		markets = append(markets, FetchedMarket{RawJSON: string(raw), Canonical: canonical})
	}

	return markets, nil
}

// normalize maps one Gamma API market into the canonical Market model.
// Category is left empty: Gamma's /markets endpoint doesn't expose
// tags/category inline (they require a separate /events/{id} lookup per
// market), so Polymarket markets rely on the title-similarity and
// date-alignment signals for matching (see docs/EQUIVALENCE.md).
func (pm polymarketMarket) normalize(fetchedAt time.Time) (normalize.Market, error) {
	var outcomes []string
	if err := json.Unmarshal([]byte(pm.Outcomes), &outcomes); err != nil {
		return normalize.Market{}, fmt.Errorf("market %s: parsing outcomes: %w", pm.ID, err)
	}
	var prices []string
	if err := json.Unmarshal([]byte(pm.OutcomePrices), &prices); err != nil {
		return normalize.Market{}, fmt.Errorf("market %s: parsing outcome prices: %w", pm.ID, err)
	}
	if len(outcomes) != len(prices) {
		return normalize.Market{}, fmt.Errorf("market %s: %d outcomes but %d prices", pm.ID, len(outcomes), len(prices))
	}

	var yesPrice, noPrice float64
	var sawYes, sawNo bool
	for i, outcome := range outcomes {
		price, err := strconv.ParseFloat(prices[i], 64)
		if err != nil {
			return normalize.Market{}, fmt.Errorf("market %s: parsing price for outcome %q: %w", pm.ID, outcome, err)
		}
		switch strings.ToLower(strings.TrimSpace(outcome)) {
		case "yes":
			yesPrice, sawYes = price, true
		case "no":
			noPrice, sawNo = price, true
		}
	}
	if !sawYes || !sawNo {
		return normalize.Market{}, fmt.Errorf("market %s: not a binary yes/no market (outcomes: %v)", pm.ID, outcomes)
	}

	resolutionDate, err := time.Parse(time.RFC3339, pm.EndDate)
	if err != nil {
		return normalize.Market{}, fmt.Errorf("market %s: parsing endDate %q: %w", pm.ID, pm.EndDate, err)
	}

	m := normalize.Market{
		ID:             normalize.ID("polymarket", pm.ID),
		Venue:          "polymarket",
		VenueMarketID:  pm.ID,
		Title:          pm.Question,
		Description:    pm.Description,
		Category:       "",
		ResolutionDate: resolutionDate,
		YesPrice:       yesPrice,
		NoPrice:        noPrice,
		Liquidity:      pm.LiquidityNum,
		FetchedAt:      fetchedAt,
	}
	if err := m.Validate(); err != nil {
		return normalize.Market{}, err
	}
	return m, nil
}
