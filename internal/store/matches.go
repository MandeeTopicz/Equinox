package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// MatchMember identifies one canonical market's participation in a matched
// cross-venue group.
type MatchMember struct {
	Venue             string `json:"venue"`
	CanonicalMarketID string `json:"canonical_market_id"`
	Title             string `json:"title"`
}

// MatchDecision is one frozen equivalence-detection decision: a group of
// canonical markets judged to represent the same event, with the composite
// score and signal breakdown that produced it (see docs/EQUIVALENCE.md).
// SignalsJSON carries the full per-pair breakdown for groups of more than
// two members; the scalar signal fields carry the group-level aggregate
// used for the show-matches table.
type MatchDecision struct {
	ID              int64
	EventID         string
	CreatedAt       time.Time
	MinScore        float64
	Score           float64
	TitleSimilarity float64
	DateAlignment   float64
	CategoryMatch   float64
	Members         []MatchMember
	SignalsJSON     string
}

// InsertMatchDecision appends a new match decision and returns its id.
// match_decisions is an event table: existing rows are never updated.
func (s *Store) InsertMatchDecision(ctx context.Context, d MatchDecision) (int64, error) {
	membersJSON, err := json.Marshal(d.Members)
	if err != nil {
		return 0, fmt.Errorf("marshaling match members: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO match_decisions
			(event_id, created_at, min_score, score, title_similarity, date_alignment, category_match, members_json, signals_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.EventID, d.CreatedAt.Format(time.RFC3339Nano), d.MinScore, d.Score,
		d.TitleSimilarity, d.DateAlignment, d.CategoryMatch, string(membersJSON), d.SignalsJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting match_decision for event %s: %w", d.EventID, err)
	}
	return res.LastInsertId()
}

// LatestMatchDecision returns the most recently recorded decision for an
// event id, or sql.ErrNoRows if none exists.
func (s *Store) LatestMatchDecision(ctx context.Context, eventID string) (MatchDecision, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, event_id, created_at, min_score, score, title_similarity, date_alignment, category_match, members_json, signals_json
		FROM match_decisions
		WHERE event_id = ?
		ORDER BY created_at DESC
		LIMIT 1`, eventID)
	return scanMatchDecision(row)
}

// ListLatestMatchDecisions returns the most recent decision per event id,
// ordered by score descending — the view `equinox show matches` renders.
func (s *Store) ListLatestMatchDecisions(ctx context.Context) ([]MatchDecision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.event_id, m.created_at, m.min_score, m.score, m.title_similarity, m.date_alignment, m.category_match, m.members_json, m.signals_json
		FROM match_decisions m
		INNER JOIN (
			SELECT event_id, MAX(created_at) AS max_created_at
			FROM match_decisions
			GROUP BY event_id
		) latest ON m.event_id = latest.event_id AND m.created_at = latest.max_created_at
		ORDER BY m.score DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying latest match_decisions: %w", err)
	}
	defer rows.Close()

	var decisions []MatchDecision
	for rows.Next() {
		d, err := scanMatchDecision(rows)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}

func scanMatchDecision(row rowScanner) (MatchDecision, error) {
	var d MatchDecision
	var createdAt, membersJSON string
	if err := row.Scan(
		&d.ID, &d.EventID, &createdAt, &d.MinScore, &d.Score,
		&d.TitleSimilarity, &d.DateAlignment, &d.CategoryMatch, &membersJSON, &d.SignalsJSON,
	); err != nil {
		return MatchDecision{}, fmt.Errorf("scanning match_decision: %w", err)
	}

	var err error
	if d.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return MatchDecision{}, fmt.Errorf("parsing created_at: %w", err)
	}
	if err := json.Unmarshal([]byte(membersJSON), &d.Members); err != nil {
		return MatchDecision{}, fmt.Errorf("unmarshaling match members: %w", err)
	}
	return d, nil
}
