package match

// Tier is the confidence classification of a match — the actual gate that
// determines whether a group is treated as equivalent, replacing a single
// compensatory --min-score threshold entirely. See docs/EQUIVALENCE.md's
// "Conjunctive tier floors" section for the reasoning.
type Tier int

const (
	TierNone        Tier = iota // doesn't qualify at all
	TierNeedsReview             // qualifies, but below the matched bar
	TierMatched                 // clears both floors comfortably
)

func (t Tier) String() string {
	switch t {
	case TierMatched:
		return "matched"
	case TierNeedsReview:
		return "needs review"
	default:
		return "none"
	}
}

// Tier floors: title similarity and resolution-date alignment must each
// independently clear the relevant floor — neither can compensate for the
// other. This directly replaces the old compensatory weighted-sum design,
// whose failure mode (a strong date-alignment signal masking a weak or
// contradictory title match) produced the real false positives found in
// live data that motivated this redesign — see docs/DECISIONS.md.
//
// These are reasoned starting points informed by this project's own
// measurements against the real OpenAI embeddings API (genuine paraphrases
// scored 0.78-0.95 title similarity; topically-adjacent-but-different text
// scored 0.50-0.70 — see docs/EQUIVALENCE.md), not fitted values from
// labeled data, which doesn't exist for this prototype. They are fixed
// constants, not a runtime flag: this project previously exposed a single
// --min-score flag for exactly this kind of tuning, and retired it
// deliberately — retuning a single number by hand, hoping to skew results
// toward a desired outcome, is the failure mode this whole redesign exists
// to move away from. Changing these numbers is a code change (with a PR
// and a documented reason), not a command-line option.
const (
	MatchedTitleFloor = 0.80
	MatchedDateFloor  = 0.90
	ReviewTitleFloor  = 0.65
	ReviewDateFloor   = 0.70
)

// ClassifyTier applies the conjunctive floors to a title-similarity/
// date-alignment pair. Used both per-candidate-pair (from a freshly
// computed Score) and per-group (from a group's min-aggregated fields) —
// the same function either way, since a group's tier should be exactly as
// conservative as applying the floors to its weakest aggregate signals.
func ClassifyTier(titleSimilarity, dateAlignment float64) Tier {
	if titleSimilarity >= MatchedTitleFloor && dateAlignment >= MatchedDateFloor {
		return TierMatched
	}
	if titleSimilarity >= ReviewTitleFloor && dateAlignment >= ReviewDateFloor {
		return TierNeedsReview
	}
	return TierNone
}
