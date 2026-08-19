// Package store owns the SQLite schema and all persistence: state tables
// (raw_markets, canonical_markets) and append-only event tables
// (match_decisions, routing_decisions). See docs/ARCHITECTURE.md for the
// state-vs-event rationale.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store is a handle to the SQLite database. It is the single source of
// truth for the pipeline; the CLI reads and writes through it and nothing
// else.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and applies
// the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}

	// SQLite handles one writer at a time; a single connection avoids
	// SQLITE_BUSY errors under this prototype's sequential CLI usage.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
