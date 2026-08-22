package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"equinox/internal/match"
	"equinox/internal/route"
	"equinox/internal/store"
)

// marketView, matchView, and decisionView are purpose-built --json shapes,
// separate from the store's row types: they use snake_case JSON tags
// consistently with the rest of the project's persisted JSON (members_json,
// signals_json, comparison_json), embed nested JSON as real objects rather
// than escaped strings, and unwrap sql.Null* into plain optional fields.
type marketView struct {
	ID             string    `json:"id"`
	Venue          string    `json:"venue"`
	VenueMarketID  string    `json:"venue_market_id"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Category       string    `json:"category"`
	ResolutionDate time.Time `json:"resolution_date"`
	YesPrice       float64   `json:"yes_price"`
	NoPrice        float64   `json:"no_price"`
	Liquidity      float64   `json:"liquidity"`
	FetchedAt      time.Time `json:"fetched_at"`
}

func toMarketView(m store.CanonicalMarket) marketView {
	return marketView{
		ID:             m.ID,
		Venue:          m.Venue,
		VenueMarketID:  m.VenueMarketID,
		Title:          m.Title,
		Description:    m.Description,
		Category:       m.Category,
		ResolutionDate: m.ResolutionDate,
		YesPrice:       m.YesPrice,
		NoPrice:        m.NoPrice,
		Liquidity:      m.Liquidity,
		FetchedAt:      m.FetchedAt,
	}
}

type matchView struct {
	EventID         string              `json:"event_id"`
	CreatedAt       time.Time           `json:"created_at"`
	Score           float64             `json:"score"`
	Tier            string              `json:"tier"`
	TitleSimilarity float64             `json:"title_similarity"`
	DateAlignment   float64             `json:"date_alignment"`
	CategoryMatch   float64             `json:"category_match"`
	Members         []store.MatchMember `json:"members"`
	Signals         json.RawMessage     `json:"signals"`
}

func toMatchView(d store.MatchDecision) matchView {
	return matchView{
		EventID:         d.EventID,
		CreatedAt:       d.CreatedAt,
		Score:           d.Score,
		Tier:            match.ClassifyTier(d.TitleSimilarity, d.DateAlignment).String(),
		TitleSimilarity: d.TitleSimilarity,
		DateAlignment:   d.DateAlignment,
		CategoryMatch:   d.CategoryMatch,
		Members:         d.Members,
		Signals:         json.RawMessage(d.SignalsJSON),
	}
}

type decisionView struct {
	EventID       string             `json:"event_id"`
	Side          string             `json:"side"`
	Size          float64            `json:"size"`
	CreatedAt     time.Time          `json:"created_at"`
	SelectedVenue string             `json:"selected_venue,omitempty"`
	IsNoop        bool               `json:"is_noop"`
	Rationale     string             `json:"rationale"`
	Quotes        []route.VenueQuote `json:"quotes,omitempty"`
}

func toDecisionView(d store.RoutingDecision) (decisionView, error) {
	v := decisionView{
		EventID:   d.EventID,
		Side:      d.Side,
		Size:      d.Size,
		CreatedAt: d.CreatedAt,
		IsNoop:    d.IsNoop,
		Rationale: d.Rationale,
	}
	if d.SelectedVenue.Valid {
		v.SelectedVenue = d.SelectedVenue.String
	}
	if d.ComparisonJSON != "" {
		if err := json.Unmarshal([]byte(d.ComparisonJSON), &v.Quotes); err != nil {
			return decisionView{}, fmt.Errorf("parsing comparison for event %s: %w", d.EventID, err)
		}
	}
	return v, nil
}
