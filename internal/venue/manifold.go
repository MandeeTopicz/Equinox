package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"equinox/internal/normalize"
)

// manifoldPageSize bounds a single fetch to one page of markets, matching
// the fetch scoping assumption in docs/ARCHITECTURE.md. 500 was confirmed to
// return a full page (no smaller cap observed).
const manifoldPageSize = 500

// ManifoldClient fetches markets from the Manifold Markets public API.
// Manifold requires no authentication (see equinox.yaml).
type ManifoldClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewManifoldClient builds a client against baseURL (e.g.
// https://api.manifold.markets). A nil httpClient uses http.DefaultClient.
func NewManifoldClient(baseURL string, httpClient *http.Client) *ManifoldClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ManifoldClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *ManifoldClient) Name() string { return "manifold" }

// manifoldMarket is the subset of /v0/markets fields this client uses.
// Manifold's bulk list endpoint doesn't include description or category/tag
// data — those live only behind a per-market /v0/market/{id} call. Rather
// than pay for one HTTP call per market, Description and Category are left
// empty here, the same tradeoff made for Polymarket's category (see
// polymarket.go). The impact is smaller for Manifold: its "question" field
// is consistently a complete, self-contained proposition (see samples in
// manifold_test.go), so matching still has a full title signal to work
// with — only the secondary description text and the tie-breaker category
// signal are unavailable.
type manifoldMarket struct {
	ID             string  `json:"id"`
	Question       string  `json:"question"`
	CloseTime      int64   `json:"closeTime"` // unix millis
	Probability    float64 `json:"probability"`
	TotalLiquidity float64 `json:"totalLiquidity"`
	OutcomeType    string  `json:"outcomeType"`
	IsResolved     bool    `json:"isResolved"`
}

func (c *ManifoldClient) FetchMarkets(ctx context.Context) ([]FetchedMarket, error) {
	url := fmt.Sprintf("%s/v0/markets?limit=%d", c.baseURL, manifoldPageSize)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building manifold request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching manifold markets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("manifold API returned %s: %s", resp.Status, string(body))
	}

	var rawMarkets []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawMarkets); err != nil {
		return nil, fmt.Errorf("decoding manifold response: %w", err)
	}

	fetchedAt := time.Now().UTC()
	markets := make([]FetchedMarket, 0, len(rawMarkets))
	for _, raw := range rawMarkets {
		var mm manifoldMarket
		if err := json.Unmarshal(raw, &mm); err != nil {
			continue // malformed entry; skip rather than fail the whole fetch
		}

		canonical, err := mm.normalize(fetchedAt)
		if err != nil {
			continue // e.g. resolved, non-binary, already closed; skip
		}

		markets = append(markets, FetchedMarket{RawJSON: string(raw), Canonical: canonical})
	}

	return markets, nil
}

// normalize maps one Manifold market into the canonical Market model.
// probability is Manifold's own P(YES) for cpmm binary markets, so it
// becomes YesPrice directly; NoPrice is its complement.
func (mm manifoldMarket) normalize(fetchedAt time.Time) (normalize.Market, error) {
	if mm.OutcomeType != "BINARY" {
		return normalize.Market{}, fmt.Errorf("market %s: not a binary market (outcomeType %q)", mm.ID, mm.OutcomeType)
	}
	if mm.IsResolved {
		return normalize.Market{}, fmt.Errorf("market %s: already resolved", mm.ID)
	}

	resolutionDate := time.UnixMilli(mm.CloseTime).UTC()
	if !resolutionDate.After(fetchedAt) {
		return normalize.Market{}, fmt.Errorf("market %s: already closed (closeTime %v)", mm.ID, resolutionDate)
	}

	m := normalize.Market{
		ID:             normalize.ID("manifold", mm.ID),
		Venue:          "manifold",
		VenueMarketID:  mm.ID,
		Title:          mm.Question,
		Description:    "",
		Category:       "",
		ResolutionDate: resolutionDate,
		YesPrice:       mm.Probability,
		NoPrice:        1 - mm.Probability,
		Liquidity:      mm.TotalLiquidity,
		FetchedAt:      fetchedAt,
	}
	if err := m.Validate(); err != nil {
		return normalize.Market{}, err
	}
	return m, nil
}
