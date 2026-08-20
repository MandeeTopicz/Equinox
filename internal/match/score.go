package match

import (
	"math"
	"strings"
	"time"

	"equinox/internal/normalize"
)

// Signal weights from docs/EQUIVALENCE.md's composite scoring table.
const (
	titleWeight    = 0.60
	dateWeight     = 0.25
	categoryWeight = 0.15
)

// Score is one candidate pair's composite similarity score and the signal
// values that produced it — logged in full, not just the final number, per
// docs/EQUIVALENCE.md ("the rationale is the breakdown, not just the final
// number").
type Score struct {
	Composite         float64
	TitleSimilarity   float64
	DateAlignment     float64
	CategoryMatch     float64 // 1 if categories matched, 0 otherwise or if not evaluated
	CategoryEvaluated bool    // false when either side lacks category data — see Composite's doc comment
}

// Composite computes a candidate pair's weighted score. titleSimilarity is
// the embedding cosine similarity, computed by the caller (Match) so this
// function stays free of any network dependency and is trivially testable.
//
// Category handling: EQUIVALENCE.md's prefilter treats a missing category
// as "doesn't block" (see Prefilter), but scoring is a different question —
// awarding score for data that doesn't exist would inflate confidence
// rather than reflect it. So when either side lacks a category (true for
// every real Polymarket or Manifold pairing — see internal/venue), the
// category weight is redistributed proportionally across title and date
// rather than guessed at either extreme. In this project's actual venue
// data, only Kalshi provides category, so this redistributed path is the
// common case in practice, not an edge case — documented in
// docs/EQUIVALENCE.md.
func Composite(a, b normalize.Market, titleSimilarity float64, dateWindow time.Duration) Score {
	date := dateAlignment(a.ResolutionDate, b.ResolutionDate, dateWindow)
	haveCategory := a.Category != "" && b.Category != ""

	var category float64
	var composite float64
	if haveCategory {
		if strings.EqualFold(a.Category, b.Category) {
			category = 1
		}
		composite = titleWeight*titleSimilarity + dateWeight*date + categoryWeight*category
	} else {
		composite = (titleWeight*titleSimilarity + dateWeight*date) / (titleWeight + dateWeight)
	}

	return Score{
		Composite:         composite,
		TitleSimilarity:   titleSimilarity,
		DateAlignment:     date,
		CategoryMatch:     category,
		CategoryEvaluated: haveCategory,
	}
}

// dateAlignment normalizes resolution-date closeness to [0,1]: 1 when the
// dates are identical, 0 at the edge of dateWindow or beyond.
func dateAlignment(a, b time.Time, dateWindow time.Duration) float64 {
	if dateWindow <= 0 {
		return 0
	}
	diff := absDateDiff(a, b)
	align := 1 - float64(diff)/float64(dateWindow)
	if align < 0 {
		align = 0
	}
	return align
}

// cosineSimilarity computes the cosine similarity of two equal-length
// vectors, in [-1,1]. Returns 0 for a zero vector or a length mismatch
// rather than panicking or dividing by zero.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
