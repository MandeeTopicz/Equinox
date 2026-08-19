package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RawMarket is one venue's unmodified market payload, kept for traceability
// alongside its canonical form.
type RawMarket struct {
	Venue         string
	VenueMarketID string
	RawJSON       string
	FetchedAt     time.Time
}

// CanonicalMarket is one venue listing normalized into the shared internal
// representation (see docs/ARCHITECTURE.md's Normalization layer).
type CanonicalMarket struct {
	ID             string // venue:venue_market_id
	Venue          string
	VenueMarketID  string
	Title          string
	Description    string
	Category       string
	ResolutionDate time.Time
	YesPrice       float64
	NoPrice        float64
	Liquidity      float64
	FetchedAt      time.Time
}

// ReplaceVenueMarkets overwrites all raw and canonical market rows for a
// single venue with the given sets, in one transaction. This is the state
// table's "current knowledge" semantic: markets no longer returned by the
// venue (closed, delisted) are dropped, not left stale.
func (s *Store) ReplaceVenueMarkets(ctx context.Context, venue string, raw []RawMarket, canonical []CanonicalMarket) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM raw_markets WHERE venue = ?`, venue); err != nil {
		return fmt.Errorf("clearing raw_markets for %s: %w", venue, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM canonical_markets WHERE venue = ?`, venue); err != nil {
		return fmt.Errorf("clearing canonical_markets for %s: %w", venue, err)
	}

	rawStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO raw_markets (venue, venue_market_id, raw_json, fetched_at)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing raw_markets insert: %w", err)
	}
	defer rawStmt.Close()

	for _, m := range raw {
		if _, err := rawStmt.ExecContext(ctx, m.Venue, m.VenueMarketID, m.RawJSON, m.FetchedAt.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("inserting raw market %s:%s: %w", m.Venue, m.VenueMarketID, err)
		}
	}

	canonicalStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO canonical_markets
			(id, venue, venue_market_id, title, description, category, resolution_date, yes_price, no_price, liquidity, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing canonical_markets insert: %w", err)
	}
	defer canonicalStmt.Close()

	for _, m := range canonical {
		if _, err := canonicalStmt.ExecContext(ctx,
			m.ID, m.Venue, m.VenueMarketID, m.Title, m.Description, m.Category,
			m.ResolutionDate.Format(time.RFC3339Nano), m.YesPrice, m.NoPrice, m.Liquidity,
			m.FetchedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("inserting canonical market %s: %w", m.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// ListCanonicalMarkets returns canonical markets, optionally filtered to one
// venue. An empty venue returns markets across all venues.
func (s *Store) ListCanonicalMarkets(ctx context.Context, venue string) ([]CanonicalMarket, error) {
	query := `
		SELECT id, venue, venue_market_id, title, description, category,
		       resolution_date, yes_price, no_price, liquidity, fetched_at
		FROM canonical_markets`
	args := []any{}
	if venue != "" {
		query += ` WHERE venue = ?`
		args = append(args, venue)
	}
	query += ` ORDER BY venue, title`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying canonical_markets: %w", err)
	}
	defer rows.Close()

	var markets []CanonicalMarket
	for rows.Next() {
		m, err := scanCanonicalMarket(rows)
		if err != nil {
			return nil, err
		}
		markets = append(markets, m)
	}
	return markets, rows.Err()
}

// GetCanonicalMarket looks up a single canonical market by its id
// (venue:venue_market_id). It returns sql.ErrNoRows if not found.
func (s *Store) GetCanonicalMarket(ctx context.Context, id string) (CanonicalMarket, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, venue, venue_market_id, title, description, category,
		       resolution_date, yes_price, no_price, liquidity, fetched_at
		FROM canonical_markets WHERE id = ?`, id)
	return scanCanonicalMarket(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCanonicalMarket(row rowScanner) (CanonicalMarket, error) {
	var m CanonicalMarket
	var resolutionDate, fetchedAt string
	if err := row.Scan(
		&m.ID, &m.Venue, &m.VenueMarketID, &m.Title, &m.Description, &m.Category,
		&resolutionDate, &m.YesPrice, &m.NoPrice, &m.Liquidity, &fetchedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return CanonicalMarket{}, err
		}
		return CanonicalMarket{}, fmt.Errorf("scanning canonical_market: %w", err)
	}

	var err error
	if m.ResolutionDate, err = time.Parse(time.RFC3339Nano, resolutionDate); err != nil {
		return CanonicalMarket{}, fmt.Errorf("parsing resolution_date: %w", err)
	}
	if m.FetchedAt, err = time.Parse(time.RFC3339Nano, fetchedAt); err != nil {
		return CanonicalMarket{}, fmt.Errorf("parsing fetched_at: %w", err)
	}
	return m, nil
}
