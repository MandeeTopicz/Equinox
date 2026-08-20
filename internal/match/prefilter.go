package match

import (
	"strings"
	"time"

	"equinox/internal/normalize"
)

// DefaultDateWindow is the resolution-date tolerance a candidate pair must
// fall within to survive the prefilter, per docs/EQUIVALENCE.md.
const DefaultDateWindow = 48 * time.Hour

// PrefilterResult explains why a candidate pair did or didn't clear the
// heuristic prefilter — the individual gate outcomes, not just the final
// verdict, so a rejected pair stays explainable.
type PrefilterResult struct {
	Passed           bool
	DifferentVenues  bool
	CategoryMatch    bool
	DateWithinWindow bool
}

// Prefilter reports whether two canonical markets clear the heuristic
// prefilter stage before any similarity scoring runs (docs/EQUIVALENCE.md
// stage 1). All gates must pass:
//
//   - the markets are from different venues — equivalence is a cross-venue
//     question by definition (see EQUIVALENCE.md's opening definition)
//   - resolution dates fall within dateWindow of each other
//   - categories match, where both venues provide one; a market whose
//     venue doesn't expose category data (Polymarket, Manifold — see
//     internal/venue) never fails this gate on category alone, per
//     EQUIVALENCE.md's "where venue metadata provides one"
func Prefilter(a, b normalize.Market, dateWindow time.Duration) PrefilterResult {
	differentVenues := a.Venue != b.Venue

	dateDiff := a.ResolutionDate.Sub(b.ResolutionDate)
	if dateDiff < 0 {
		dateDiff = -dateDiff
	}
	dateWithinWindow := dateDiff <= dateWindow

	categoryMatch := a.Category == "" || b.Category == "" || strings.EqualFold(a.Category, b.Category)

	return PrefilterResult{
		Passed:           differentVenues && dateWithinWindow && categoryMatch,
		DifferentVenues:  differentVenues,
		CategoryMatch:    categoryMatch,
		DateWithinWindow: dateWithinWindow,
	}
}
