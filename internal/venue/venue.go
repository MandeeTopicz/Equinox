// Package venue defines the VenueClient interface and per-venue implementations
// (Polymarket, Kalshi, Manifold). All venue-specific request/response handling
// is confined to this package.
package venue

import (
	"context"

	"equinox/internal/normalize"
)

// VenueClient fetches markets from one prediction market venue. Everything
// venue-specific — auth, endpoint shape, field parsing — lives inside an
// implementation of this interface and nowhere else, per
// docs/ARCHITECTURE.md's layering.
type VenueClient interface {
	// Name returns the venue's identifier, matching its name in equinox.yaml
	// and normalize.Market.Venue.
	Name() string

	// FetchMarkets retrieves the venue's active/open markets. Markets that
	// fail normalization (malformed data, non-binary outcomes) are skipped
	// rather than surfaced as an error — this is the graceful degradation on
	// incomplete or ambiguous venue data required by the PRD.
	FetchMarkets(ctx context.Context) ([]FetchedMarket, error)
}

// FetchedMarket pairs one venue market's unmodified raw payload with its
// normalized canonical form, ready for store.ReplaceVenueMarkets.
type FetchedMarket struct {
	RawJSON   string
	Canonical normalize.Market
}
