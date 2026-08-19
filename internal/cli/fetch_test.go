package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"equinox/internal/normalize"
	"equinox/internal/store"
	"equinox/internal/venue"
)

type fakeVenueClient struct {
	name    string
	markets []venue.FetchedMarket
	err     error
}

func (f fakeVenueClient) Name() string { return f.name }

func (f fakeVenueClient) FetchMarkets(ctx context.Context) ([]venue.FetchedMarket, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.markets, nil
}

type storeCall struct {
	venue     string
	rawCount  int
	canonical []store.CanonicalMarket
}

type fakeStore struct {
	calls   []storeCall
	failFor string
}

func (f *fakeStore) ReplaceVenueMarkets(ctx context.Context, venueName string, raw []store.RawMarket, canonical []store.CanonicalMarket) error {
	if venueName == f.failFor {
		return errors.New("simulated store failure")
	}
	f.calls = append(f.calls, storeCall{venue: venueName, rawCount: len(raw), canonical: canonical})
	return nil
}

func marketFixture(venueName, id, title string) venue.FetchedMarket {
	return venue.FetchedMarket{
		RawJSON: `{"id":"` + id + `"}`,
		Canonical: normalize.Market{
			ID:             normalize.ID(venueName, id),
			Venue:          venueName,
			VenueMarketID:  id,
			Title:          title,
			ResolutionDate: time.Now().Add(24 * time.Hour),
			YesPrice:       0.5,
			NoPrice:        0.5,
			Liquidity:      100,
			FetchedAt:      time.Now(),
		},
	}
}

func TestFetchSuccess(t *testing.T) {
	clients := []venue.VenueClient{
		fakeVenueClient{name: "polymarket", markets: []venue.FetchedMarket{
			marketFixture("polymarket", "1", "Market A"),
			marketFixture("polymarket", "2", "Market B"),
		}},
		fakeVenueClient{name: "kalshi", markets: []venue.FetchedMarket{
			marketFixture("kalshi", "X", "Market C"),
		}},
	}
	st := &fakeStore{}
	var out bytes.Buffer

	err := Fetch(context.Background(), FetchDeps{Venues: clients, Store: st, Out: &out})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	wantSummary := "fetched 2 markets from polymarket, 1 from kalshi\n"
	if out.String() != wantSummary {
		t.Errorf("summary = %q, want %q", out.String(), wantSummary)
	}

	if len(st.calls) != 2 {
		t.Fatalf("expected 2 store calls, got %d", len(st.calls))
	}
	if st.calls[0].venue != "polymarket" || st.calls[0].rawCount != 2 {
		t.Errorf("unexpected first store call: %+v", st.calls[0])
	}
	if st.calls[1].venue != "kalshi" || st.calls[1].rawCount != 1 {
		t.Errorf("unexpected second store call: %+v", st.calls[1])
	}
	if st.calls[0].canonical[0].Title != "Market A" {
		t.Errorf("canonical conversion lost data: %+v", st.calls[0].canonical[0])
	}
}

func TestFetchPartialVenueFailureIsAWarningNotAFatalError(t *testing.T) {
	clients := []venue.VenueClient{
		fakeVenueClient{name: "polymarket", markets: []venue.FetchedMarket{marketFixture("polymarket", "1", "Market A")}},
		fakeVenueClient{name: "kalshi", err: errors.New("connection refused")},
	}
	st := &fakeStore{}
	var out bytes.Buffer

	err := Fetch(context.Background(), FetchDeps{Venues: clients, Store: st, Out: &out})
	if err != nil {
		t.Fatalf("expected partial failure to not be fatal, got: %v", err)
	}

	if len(st.calls) != 1 || st.calls[0].venue != "polymarket" {
		t.Errorf("expected only polymarket to be stored, got: %+v", st.calls)
	}

	output := out.String()
	if !bytes.Contains([]byte(output), []byte("fetched 1 markets from polymarket")) {
		t.Errorf("expected success summary for polymarket, got: %q", output)
	}
	if !bytes.Contains([]byte(output), []byte("warning: kalshi: connection refused")) {
		t.Errorf("expected warning for kalshi, got: %q", output)
	}
}

func TestFetchAllVenuesFailingIsFatal(t *testing.T) {
	clients := []venue.VenueClient{
		fakeVenueClient{name: "polymarket", err: errors.New("timeout")},
		fakeVenueClient{name: "kalshi", err: errors.New("timeout")},
	}
	st := &fakeStore{}
	var out bytes.Buffer

	err := Fetch(context.Background(), FetchDeps{Venues: clients, Store: st, Out: &out})
	if err == nil {
		t.Fatal("expected an error when every venue fails, got nil")
	}
}

func TestFetchStoreFailureIsAWarning(t *testing.T) {
	clients := []venue.VenueClient{
		fakeVenueClient{name: "polymarket", markets: []venue.FetchedMarket{marketFixture("polymarket", "1", "Market A")}},
		fakeVenueClient{name: "kalshi", markets: []venue.FetchedMarket{marketFixture("kalshi", "X", "Market C")}},
	}
	st := &fakeStore{failFor: "kalshi"}
	var out bytes.Buffer

	err := Fetch(context.Background(), FetchDeps{Venues: clients, Store: st, Out: &out})
	if err != nil {
		t.Fatalf("expected partial store failure to not be fatal, got: %v", err)
	}
	if len(st.calls) != 1 {
		t.Fatalf("expected 1 successful store call, got %d", len(st.calls))
	}
}
