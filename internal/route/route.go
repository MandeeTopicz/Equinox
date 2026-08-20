// Package route implements the venue-agnostic routing engine: given a matched
// group of canonical markets and a hypothetical order, it selects a venue and
// records the rationale.
package route

import (
	"fmt"
	"strings"

	"equinox/internal/normalize"
)

// VenueQuote is one venue's evaluation for a requested order, read only
// from canonical fields that exist identically across every venue
// implementation (docs/ROUTING.md) — Route never branches on venue
// identity or name.
type VenueQuote struct {
	Venue       string  `json:"venue"`
	Price       float64 `json:"price"`
	Liquidity   float64 `json:"liquidity"`
	LiquidityOK bool    `json:"liquidity_ok"`
	Selected    bool    `json:"selected"`
}

// Decision is the outcome of evaluating a group of markets for a
// hypothetical order.
type Decision struct {
	Side          string
	Size          float64
	Quotes        []VenueQuote
	SelectedVenue string // empty if no venue's liquidity supports the order
	Rationale     string
}

// Route evaluates every market in members for the given side and size, and
// selects the venue with the best (lowest) price among those whose
// liquidity proxy plausibly supports size; ties are broken on higher
// liquidity. This is the single deterministic rule described in
// docs/ROUTING.md — no order-book depth simulation, no fee modeling.
//
// "Plausibly supports size" is Liquidity >= size. Venues' liquidity
// proxies are different units by nature (Kalshi: contract size at the
// quoted price; Polymarket/Manifold: an AMM pool metric) — ROUTING.md
// explicitly scopes reconciling that out ("a simple liquidity proxy...
// not a full order-book depth simulation"), so this stays a direct
// comparison rather than an attempted unit conversion.
func Route(members []normalize.Market, side string, size float64) (Decision, error) {
	if len(members) == 0 {
		return Decision{}, fmt.Errorf("route: no markets to evaluate")
	}

	quotes := make([]VenueQuote, len(members))
	bestIdx := -1
	for i, m := range members {
		price, err := m.Price(side)
		if err != nil {
			return Decision{}, fmt.Errorf("route: %w", err)
		}

		liquidityOK := m.Liquidity >= size
		quotes[i] = VenueQuote{Venue: m.Venue, Price: price, Liquidity: m.Liquidity, LiquidityOK: liquidityOK}

		if !liquidityOK {
			continue
		}
		if bestIdx == -1 || price < quotes[bestIdx].Price ||
			(price == quotes[bestIdx].Price && m.Liquidity > quotes[bestIdx].Liquidity) {
			bestIdx = i
		}
	}

	var selectedVenue string
	if bestIdx >= 0 {
		quotes[bestIdx].Selected = true
		selectedVenue = quotes[bestIdx].Venue
	}

	return Decision{
		Side:          side,
		Size:          size,
		Quotes:        quotes,
		SelectedVenue: selectedVenue,
		Rationale:     rationale(quotes, side),
	}, nil
}

func rationale(quotes []VenueQuote, side string) string {
	var selected VenueQuote
	var found bool
	var otherPrices []string
	var excludedOnLiquidity []string

	for _, q := range quotes {
		switch {
		case q.Selected:
			selected, found = q, true
		case !q.LiquidityOK:
			excludedOnLiquidity = append(excludedOnLiquidity, q.Venue)
		default:
			otherPrices = append(otherPrices, fmt.Sprintf("%s %.2f", q.Venue, q.Price))
		}
	}

	if !found {
		return fmt.Sprintf("no venue could support the requested size on the %s side; all excluded on liquidity", side)
	}

	msg := fmt.Sprintf("selected: %s — best %s price at requested size (%.2f", selected.Venue, strings.ToUpper(side), selected.Price)
	if len(otherPrices) > 0 {
		msg += " vs. " + strings.Join(otherPrices, ", ")
	}
	msg += ")"
	if len(excludedOnLiquidity) > 0 {
		msg += fmt.Sprintf("; %s excluded on liquidity", strings.Join(excludedOnLiquidity, ", "))
	}
	return msg
}
