package main

import (
	"fmt"
	"net/http"
	"time"

	"equinox/internal/venue"
)

// httpClientTimeout bounds each venue request so a slow/unreachable venue
// can't hang the whole fetch indefinitely.
const httpClientTimeout = 30 * time.Second

// buildVenueClients constructs a VenueClient for each entry in cfg.Venues.
// This is the one place venue names are dispatched on outside internal/venue
// itself — necessary wiring, not the venue-specific branching CLAUDE.md
// forbids in normalize/match/route.
func buildVenueClients(cfg *Config) ([]venue.VenueClient, error) {
	httpClient := &http.Client{Timeout: httpClientTimeout}

	clients := make([]venue.VenueClient, 0, len(cfg.Venues))
	for _, v := range cfg.Venues {
		switch v.Name {
		case "polymarket":
			clients = append(clients, venue.NewPolymarketClient(v.BaseURL, httpClient))
		case "kalshi":
			clients = append(clients, venue.NewKalshiClient(v.BaseURL, httpClient))
		case "manifold":
			clients = append(clients, venue.NewManifoldClient(v.BaseURL, httpClient))
		default:
			return nil, fmt.Errorf("equinox.yaml: unknown venue %q", v.Name)
		}
	}
	return clients, nil
}
