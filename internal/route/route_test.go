package route

import (
	"strings"
	"testing"

	"equinox/internal/normalize"
)

func market(venue string, yesPrice, noPrice, liquidity float64) normalize.Market {
	return normalize.Market{Venue: venue, YesPrice: yesPrice, NoPrice: noPrice, Liquidity: liquidity}
}

func TestRouteSelectsBestPriceAmongLiquidVenues(t *testing.T) {
	// Matches the scenario in README.md's example session.
	members := []normalize.Market{
		market("kalshi", 0.62, 0.38, 1000),
		market("polymarket", 0.65, 0.35, 1000),
		market("manifold", 0.60, 0.40, 50), // cheapest price, but insufficient liquidity
	}

	d, err := Route(members, "yes", 100)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	if d.SelectedVenue != "kalshi" {
		t.Errorf("SelectedVenue = %q, want kalshi", d.SelectedVenue)
	}
	if len(d.Quotes) != 3 {
		t.Fatalf("expected 3 quotes, got %d", len(d.Quotes))
	}

	byVenue := map[string]VenueQuote{}
	for _, q := range d.Quotes {
		byVenue[q.Venue] = q
	}
	if !byVenue["kalshi"].Selected {
		t.Error("expected kalshi to be selected")
	}
	if !byVenue["kalshi"].LiquidityOK {
		t.Error("expected kalshi liquidity to be OK")
	}
	if byVenue["manifold"].LiquidityOK {
		t.Error("expected manifold liquidity to be insufficient")
	}
	if byVenue["manifold"].Selected {
		t.Error("manifold should not be selected despite the lowest price — insufficient liquidity")
	}
	if byVenue["polymarket"].Selected {
		t.Error("polymarket should not be selected — kalshi has a better price")
	}

	if !strings.Contains(d.Rationale, "selected: kalshi") {
		t.Errorf("rationale missing selection: %q", d.Rationale)
	}
	if !strings.Contains(d.Rationale, "manifold excluded on liquidity") {
		t.Errorf("rationale missing liquidity exclusion: %q", d.Rationale)
	}
}

func TestRouteNoSide(t *testing.T) {
	members := []normalize.Market{
		market("kalshi", 0.62, 0.38, 1000),
		market("polymarket", 0.65, 0.35, 1000),
	}

	d, err := Route(members, "no", 100)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	// Lower NO price wins: polymarket 0.35 < kalshi 0.38.
	if d.SelectedVenue != "polymarket" {
		t.Errorf("SelectedVenue = %q, want polymarket", d.SelectedVenue)
	}
}

func TestRouteTiesBreakOnHigherLiquidity(t *testing.T) {
	members := []normalize.Market{
		market("kalshi", 0.50, 0.50, 100),
		market("polymarket", 0.50, 0.50, 500),
	}

	d, err := Route(members, "yes", 10)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.SelectedVenue != "polymarket" {
		t.Errorf("SelectedVenue = %q, want polymarket (higher liquidity tiebreak)", d.SelectedVenue)
	}
}

func TestRouteNoVenueHasSufficientLiquidity(t *testing.T) {
	members := []normalize.Market{
		market("kalshi", 0.50, 0.50, 5),
		market("polymarket", 0.55, 0.45, 5),
	}

	d, err := Route(members, "yes", 1000)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.SelectedVenue != "" {
		t.Errorf("expected no venue selected, got %q", d.SelectedVenue)
	}
	for _, q := range d.Quotes {
		if q.Selected {
			t.Errorf("no quote should be marked selected: %+v", q)
		}
	}
	if !strings.Contains(d.Rationale, "no venue could support") {
		t.Errorf("expected a no-venue-qualifies rationale, got %q", d.Rationale)
	}
}

func TestRouteEmptyMembers(t *testing.T) {
	if _, err := Route(nil, "yes", 100); err == nil {
		t.Fatal("expected an error for an empty member list, got nil")
	}
}

func TestRouteInvalidSide(t *testing.T) {
	members := []normalize.Market{market("kalshi", 0.5, 0.5, 100)}
	if _, err := Route(members, "maybe", 100); err == nil {
		t.Fatal("expected an error for an invalid side, got nil")
	}
}

func TestRouteSingleMember(t *testing.T) {
	members := []normalize.Market{market("kalshi", 0.62, 0.38, 1000)}

	d, err := Route(members, "yes", 100)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.SelectedVenue != "kalshi" {
		t.Errorf("SelectedVenue = %q, want kalshi", d.SelectedVenue)
	}
	if strings.Contains(d.Rationale, "vs.") {
		t.Errorf("rationale shouldn't compare against anything with only one venue: %q", d.Rationale)
	}
}
