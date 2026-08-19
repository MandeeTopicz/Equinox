package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RoutingDecision is one frozen routing decision: a hypothetical order's
// evaluation across a matched group's venues, the venue selected (if any),
// and why (see docs/ROUTING.md).
type RoutingDecision struct {
	ID              int64
	MatchDecisionID sql.NullInt64 // the match_decisions row this relied on; unset for a no-match-group no-op
	EventID         string
	Side            string
	Size            float64
	CreatedAt       time.Time
	SelectedVenue   sql.NullString // unset when IsNoop and no venue qualifies
	IsNoop          bool           // true when --event has no match group to compare against
	Rationale       string
	ComparisonJSON  string // full per-venue comparison table (see docs/ROUTING.md's rationale format)
}

// InsertRoutingDecision appends a new routing decision and returns its id.
// routing_decisions is an event table: existing rows are never updated.
func (s *Store) InsertRoutingDecision(ctx context.Context, d RoutingDecision) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO routing_decisions
			(match_decision_id, event_id, side, size, created_at, selected_venue, is_noop, rationale, comparison_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.MatchDecisionID, d.EventID, d.Side, d.Size, d.CreatedAt.Format(time.RFC3339Nano),
		d.SelectedVenue, d.IsNoop, d.Rationale, d.ComparisonJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting routing_decision for event %s: %w", d.EventID, err)
	}
	return res.LastInsertId()
}

// ListRoutingDecisions returns routing decisions, optionally filtered to one
// event id, most recent first. An empty eventID returns decisions across all
// events.
func (s *Store) ListRoutingDecisions(ctx context.Context, eventID string) ([]RoutingDecision, error) {
	query := `
		SELECT id, match_decision_id, event_id, side, size, created_at, selected_venue, is_noop, rationale, comparison_json
		FROM routing_decisions`
	args := []any{}
	if eventID != "" {
		query += ` WHERE event_id = ?`
		args = append(args, eventID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying routing_decisions: %w", err)
	}
	defer rows.Close()

	var decisions []RoutingDecision
	for rows.Next() {
		d, err := scanRoutingDecision(rows)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}

func scanRoutingDecision(row rowScanner) (RoutingDecision, error) {
	var d RoutingDecision
	var createdAt string
	if err := row.Scan(
		&d.ID, &d.MatchDecisionID, &d.EventID, &d.Side, &d.Size, &createdAt,
		&d.SelectedVenue, &d.IsNoop, &d.Rationale, &d.ComparisonJSON,
	); err != nil {
		return RoutingDecision{}, fmt.Errorf("scanning routing_decision: %w", err)
	}

	var err error
	if d.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return RoutingDecision{}, fmt.Errorf("parsing created_at: %w", err)
	}
	return d, nil
}
