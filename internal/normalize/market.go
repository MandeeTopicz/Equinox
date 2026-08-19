package normalize

import (
	"fmt"
	"strings"
	"time"
)

// Market is the canonical, venue-agnostic representation of one venue's
// listing for a binary (yes/no) market. It is the only shape that flows
// through equivalence detection and routing — those layers never see a raw
// venue payload (see docs/ARCHITECTURE.md).
type Market struct {
	ID             string // canonical id: venue:venue_market_id, see ID()
	Venue          string
	VenueMarketID  string // the venue's native ticker/id, kept for traceability
	Title          string
	Description    string
	Category       string
	ResolutionDate time.Time
	YesPrice       float64
	NoPrice        float64
	Liquidity      float64 // liquidity proxy: available size at the quoted price, or venue-reported volume/liquidity metadata
	FetchedAt      time.Time
}

// ID builds the canonical market id from a venue name and that venue's
// native market id. Defined once here so every venue client and caller
// constructs it identically.
func ID(venue, venueMarketID string) string {
	return venue + ":" + venueMarketID
}

// Validate reports whether m has the minimum fields required to participate
// in matching and routing. Venue clients call this after normalizing a raw
// payload and skip markets that fail it — degrading gracefully on
// incomplete or ambiguous venue data rather than storing it (PRD
// requirement 6) or failing the whole fetch.
func (m Market) Validate() error {
	var problems []string

	if m.Venue == "" {
		problems = append(problems, "venue is empty")
	}
	if m.VenueMarketID == "" {
		problems = append(problems, "venue market id is empty")
	}
	if m.ID != ID(m.Venue, m.VenueMarketID) {
		problems = append(problems, "id does not match venue:venue_market_id")
	}
	if m.Title == "" {
		problems = append(problems, "title is empty")
	}
	if m.ResolutionDate.IsZero() {
		problems = append(problems, "resolution date is zero")
	}
	if m.YesPrice < 0 || m.YesPrice > 1 {
		problems = append(problems, fmt.Sprintf("yes price %v is outside [0,1]", m.YesPrice))
	}
	if m.NoPrice < 0 || m.NoPrice > 1 {
		problems = append(problems, fmt.Sprintf("no price %v is outside [0,1]", m.NoPrice))
	}
	if m.Liquidity < 0 {
		problems = append(problems, fmt.Sprintf("liquidity %v is negative", m.Liquidity))
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid market %q: %s", m.ID, strings.Join(problems, "; "))
	}
	return nil
}

// Price returns the normalized price for the requested side ("yes" or
// "no"), as required by routing (docs/ROUTING.md).
func (m Market) Price(side string) (float64, error) {
	switch side {
	case "yes":
		return m.YesPrice, nil
	case "no":
		return m.NoPrice, nil
	default:
		return 0, fmt.Errorf("invalid side %q: must be \"yes\" or \"no\"", side)
	}
}
