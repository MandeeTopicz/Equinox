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
	dateWithinWindow := absDateDiff(a.ResolutionDate, b.ResolutionDate) <= dateWindow

	categoryMatch := a.Category == "" || b.Category == "" || strings.EqualFold(a.Category, b.Category)

	return PrefilterResult{
		Passed:           differentVenues && dateWithinWindow && categoryMatch,
		DifferentVenues:  differentVenues,
		CategoryMatch:    categoryMatch,
		DateWithinWindow: dateWithinWindow,
	}
}

// absDateDiff returns the non-negative gap between two times. It always
// subtracts in the later-minus-earlier direction rather than subtracting
// and then negating a possibly-negative result: time.Time.Sub clamps to
// time.Duration's minimum representable value for a sufficiently large gap
// (some real markets resolve centuries out — e.g. "will X ever happen"
// questions), and negating that minimum value overflows back to itself
// under Go's signed two's complement arithmetic, silently producing a
// large *negative* "absolute" difference instead of a large positive one.
// Always computing the non-negative direction avoids that entirely.
func absDateDiff(a, b time.Time) time.Duration {
	if a.Before(b) {
		return b.Sub(a)
	}
	return a.Sub(b)
}
