package normalize

import (
	"testing"
	"time"
)

func validMarket() Market {
	return Market{
		ID:             ID("polymarket", "12345"),
		Venue:          "polymarket",
		VenueMarketID:  "12345",
		Title:          "Will the Fed cut rates in March?",
		Description:    "Resolves YES if the FOMC cuts the federal funds rate at its March 2026 meeting.",
		Category:       "economics",
		ResolutionDate: time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC),
		YesPrice:       0.65,
		NoPrice:        0.35,
		Liquidity:      1000,
		FetchedAt:      time.Now(),
	}
}

func TestID(t *testing.T) {
	if got, want := ID("kalshi", "FED-MAR"), "kalshi:FED-MAR"; got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}
}

func TestMarketValidate(t *testing.T) {
	if err := validMarket().Validate(); err != nil {
		t.Errorf("expected valid market to pass, got: %v", err)
	}

	tests := []struct {
		name   string
		modify func(*Market)
	}{
		{"empty venue", func(m *Market) { m.Venue = "" }},
		{"empty venue market id", func(m *Market) { m.VenueMarketID = "" }},
		{"id mismatch", func(m *Market) { m.ID = "wrong:id" }},
		{"empty title", func(m *Market) { m.Title = "" }},
		{"zero resolution date", func(m *Market) { m.ResolutionDate = time.Time{} }},
		{"yes price too high", func(m *Market) { m.YesPrice = 1.5 }},
		{"yes price negative", func(m *Market) { m.YesPrice = -0.1 }},
		{"no price too high", func(m *Market) { m.NoPrice = 1.5 }},
		{"negative liquidity", func(m *Market) { m.Liquidity = -5 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMarket()
			tt.modify(&m)
			if err := m.Validate(); err == nil {
				t.Errorf("expected validation error for %s, got nil", tt.name)
			}
		})
	}
}

func TestMarketPrice(t *testing.T) {
	m := validMarket()

	yes, err := m.Price("yes")
	if err != nil || yes != 0.65 {
		t.Errorf("Price(yes) = %v, %v; want 0.65, nil", yes, err)
	}

	no, err := m.Price("no")
	if err != nil || no != 0.35 {
		t.Errorf("Price(no) = %v, %v; want 0.35, nil", no, err)
	}

	if _, err := m.Price("maybe"); err == nil {
		t.Error("expected error for invalid side, got nil")
	}
}
