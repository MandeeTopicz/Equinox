package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"equinox/internal/store"
	"equinox/internal/venue"
)

// FetchStore is the subset of *store.Store the fetch command needs.
type FetchStore interface {
	ReplaceVenueMarkets(ctx context.Context, venueName string, raw []store.RawMarket, canonical []store.CanonicalMarket) error
}

// FetchDeps holds fetch's constructed dependencies. cmd/equinox builds these
// from equinox.yaml; this package stays config-format-agnostic.
type FetchDeps struct {
	Venues []venue.VenueClient
	Store  FetchStore
	Out    io.Writer
}

// Fetch ingests markets from every configured venue into the store: venue
// clients -> normalization (already done by the client) -> store. A venue
// whose fetch fails is skipped with a warning rather than aborting the
// whole run, per the PRD's requirement to degrade gracefully rather than
// fail outright — unless every venue fails, in which case there is nothing
// useful to report and Fetch returns an error.
func Fetch(ctx context.Context, deps FetchDeps) error {
	type result struct {
		venue string
		count int
	}
	var results []result
	var warnings []string

	for _, client := range deps.Venues {
		fetched, err := client.FetchMarkets(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", client.Name(), err))
			continue
		}

		raw := make([]store.RawMarket, len(fetched))
		canonical := make([]store.CanonicalMarket, len(fetched))
		for i, fm := range fetched {
			raw[i] = store.RawMarket{
				Venue:         client.Name(),
				VenueMarketID: fm.Canonical.VenueMarketID,
				RawJSON:       fm.RawJSON,
				FetchedAt:     fm.Canonical.FetchedAt,
			}
			canonical[i] = toStoreCanonicalMarket(fm.Canonical)
		}

		if err := deps.Store.ReplaceVenueMarkets(ctx, client.Name(), raw, canonical); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: storing markets: %v", client.Name(), err))
			continue
		}

		results = append(results, result{venue: client.Name(), count: len(fetched)})
	}

	if len(results) == 0 {
		return fmt.Errorf("fetch failed for all venues: %s", strings.Join(warnings, "; "))
	}

	parts := make([]string, len(results))
	for i, r := range results {
		if i == 0 {
			parts[i] = fmt.Sprintf("%d markets from %s", r.count, r.venue)
		} else {
			parts[i] = fmt.Sprintf("%d from %s", r.count, r.venue)
		}
	}
	fmt.Fprintf(deps.Out, "fetched %s\n", strings.Join(parts, ", "))

	for _, w := range warnings {
		fmt.Fprintf(deps.Out, "warning: %s\n", w)
	}

	return nil
}
