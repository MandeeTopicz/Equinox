package match

import (
	"testing"
	"time"

	"equinox/internal/normalize"
)

func marketAt(venue, category string, resolutionDate time.Time) normalize.Market {
	return normalize.Market{Venue: venue, Category: category, ResolutionDate: resolutionDate}
}

func TestPrefilter(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		a, b normalize.Market
		want PrefilterResult
	}{
		{
			name: "same venue always fails regardless of other signals",
			a:    marketAt("polymarket", "econ", base),
			b:    marketAt("polymarket", "econ", base),
			want: PrefilterResult{Passed: false, DifferentVenues: false, CategoryMatch: true, DateWithinWindow: true},
		},
		{
			name: "different venues, matching category, same date passes",
			a:    marketAt("polymarket", "econ", base),
			b:    marketAt("kalshi", "econ", base),
			want: PrefilterResult{Passed: true, DifferentVenues: true, CategoryMatch: true, DateWithinWindow: true},
		},
		{
			name: "category comparison is case-insensitive",
			a:    marketAt("polymarket", "Economics", base),
			b:    marketAt("kalshi", "ECONOMICS", base),
			want: PrefilterResult{Passed: true, DifferentVenues: true, CategoryMatch: true, DateWithinWindow: true},
		},
		{
			name: "differing non-empty categories fail the gate",
			a:    marketAt("polymarket", "econ", base),
			b:    marketAt("kalshi", "sports", base),
			want: PrefilterResult{Passed: false, DifferentVenues: true, CategoryMatch: false, DateWithinWindow: true},
		},
		{
			name: "empty category on one side never blocks the gate",
			a:    marketAt("polymarket", "", base), // Polymarket never provides category, see internal/venue/polymarket.go
			b:    marketAt("kalshi", "sports", base),
			want: PrefilterResult{Passed: true, DifferentVenues: true, CategoryMatch: true, DateWithinWindow: true},
		},
		{
			name: "empty category on both sides never blocks the gate",
			a:    marketAt("polymarket", "", base),
			b:    marketAt("manifold", "", base),
			want: PrefilterResult{Passed: true, DifferentVenues: true, CategoryMatch: true, DateWithinWindow: true},
		},
		{
			name: "dates exactly at the window boundary pass (inclusive)",
			a:    marketAt("polymarket", "econ", base),
			b:    marketAt("kalshi", "econ", base.Add(DefaultDateWindow)),
			want: PrefilterResult{Passed: true, DifferentVenues: true, CategoryMatch: true, DateWithinWindow: true},
		},
		{
			name: "dates one second past the window boundary fail",
			a:    marketAt("polymarket", "econ", base),
			b:    marketAt("kalshi", "econ", base.Add(DefaultDateWindow+time.Second)),
			want: PrefilterResult{Passed: false, DifferentVenues: true, CategoryMatch: true, DateWithinWindow: false},
		},
		{
			name: "date window is symmetric regardless of which side is earlier",
			a:    marketAt("polymarket", "econ", base.Add(DefaultDateWindow)),
			b:    marketAt("kalshi", "econ", base),
			want: PrefilterResult{Passed: true, DifferentVenues: true, CategoryMatch: true, DateWithinWindow: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Prefilter(tt.a, tt.b, DefaultDateWindow)
			if got != tt.want {
				t.Errorf("Prefilter() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPrefilterCustomDateWindow(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	a := marketAt("polymarket", "econ", base)
	b := marketAt("kalshi", "econ", base.Add(2*time.Hour))

	if got := Prefilter(a, b, time.Hour); got.Passed {
		t.Error("expected a 1h window to reject a 2h date gap")
	}
	if got := Prefilter(a, b, 3*time.Hour); !got.Passed {
		t.Error("expected a 3h window to accept a 2h date gap")
	}
}
