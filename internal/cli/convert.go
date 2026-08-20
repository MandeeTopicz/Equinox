package cli

import (
	"equinox/internal/normalize"
	"equinox/internal/store"
)

// toStoreCanonicalMarket converts the domain model (used by internal/match,
// internal/route) into the store's row shape (used by internal/store).
// Kept separate from normalize.Market on purpose: store stays free of any
// dependency on business-domain types (see docs/ARCHITECTURE.md).
func toStoreCanonicalMarket(m normalize.Market) store.CanonicalMarket {
	return store.CanonicalMarket{
		ID:             m.ID,
		Venue:          m.Venue,
		VenueMarketID:  m.VenueMarketID,
		Title:          m.Title,
		Description:    m.Description,
		Category:       m.Category,
		ResolutionDate: m.ResolutionDate,
		YesPrice:       m.YesPrice,
		NoPrice:        m.NoPrice,
		Liquidity:      m.Liquidity,
		FetchedAt:      m.FetchedAt,
	}
}

// toNormalizeMarket is the inverse of toStoreCanonicalMarket, used when
// reading canonical markets back out of the store for matching/routing.
func toNormalizeMarket(m store.CanonicalMarket) normalize.Market {
	return normalize.Market{
		ID:             m.ID,
		Venue:          m.Venue,
		VenueMarketID:  m.VenueMarketID,
		Title:          m.Title,
		Description:    m.Description,
		Category:       m.Category,
		ResolutionDate: m.ResolutionDate,
		YesPrice:       m.YesPrice,
		NoPrice:        m.NoPrice,
		Liquidity:      m.Liquidity,
		FetchedAt:      m.FetchedAt,
	}
}
