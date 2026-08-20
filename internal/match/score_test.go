package match

import (
	"math"
	"testing"
	"time"
)

func TestCompositeBothCategoriesMatch(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	a := marketAt("kalshi", "econ", base)
	b := marketAt("polymarket", "econ", base) // same date, same category, title sim 1.0

	got := Composite(a, b, 1.0, DefaultDateWindow)
	if !got.CategoryEvaluated {
		t.Fatal("expected CategoryEvaluated true when both sides have a category")
	}
	if got.CategoryMatch != 1 {
		t.Errorf("CategoryMatch = %v, want 1", got.CategoryMatch)
	}
	want := titleWeight*1.0 + dateWeight*1.0 + categoryWeight*1.0
	if math.Abs(got.Composite-want) > 1e-9 {
		t.Errorf("Composite = %v, want %v", got.Composite, want)
	}
}

func TestCompositeBothCategoriesDiffer(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	a := marketAt("kalshi", "econ", base)
	b := marketAt("polymarket", "sports", base)

	got := Composite(a, b, 1.0, DefaultDateWindow)
	if !got.CategoryEvaluated {
		t.Fatal("expected CategoryEvaluated true when both sides have a category")
	}
	if got.CategoryMatch != 0 {
		t.Errorf("CategoryMatch = %v, want 0", got.CategoryMatch)
	}
	want := titleWeight*1.0 + dateWeight*1.0 + categoryWeight*0.0
	if math.Abs(got.Composite-want) > 1e-9 {
		t.Errorf("Composite = %v, want %v", got.Composite, want)
	}
}

func TestCompositeMissingCategoryRedistributesWeight(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	a := marketAt("polymarket", "", base) // Polymarket never has category
	b := marketAt("manifold", "", base)

	got := Composite(a, b, 1.0, DefaultDateWindow)
	if got.CategoryEvaluated {
		t.Fatal("expected CategoryEvaluated false when a side lacks category")
	}
	// Weight is redistributed across title+date, so a perfect title+date
	// match should reach a perfect composite score even without category.
	if math.Abs(got.Composite-1.0) > 1e-9 {
		t.Errorf("Composite = %v, want 1.0 (full weight on title+date)", got.Composite)
	}
}

func TestDateAlignment(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)

	if got := dateAlignment(base, base, DefaultDateWindow); got != 1 {
		t.Errorf("identical dates: got %v, want 1", got)
	}
	if got := dateAlignment(base, base.Add(DefaultDateWindow), DefaultDateWindow); got != 0 {
		t.Errorf("dates at window edge: got %v, want 0", got)
	}
	if got := dateAlignment(base, base.Add(2*DefaultDateWindow), DefaultDateWindow); got != 0 {
		t.Errorf("dates past window: got %v, want 0 (clamped)", got)
	}
	half := dateAlignment(base, base.Add(DefaultDateWindow/2), DefaultDateWindow)
	if math.Abs(half-0.5) > 1e-9 {
		t.Errorf("dates at half the window: got %v, want 0.5", half)
	}
}

// Regression test for the same overflow bug covered in
// TestPrefilterExtremeDateGapDoesNotOverflow: a multi-century date gap must
// clamp to 0, not wrap around into a huge positive value.
func TestDateAlignmentExtremeDateGapDoesNotOverflow(t *testing.T) {
	near := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	farFuture := time.Date(4133, 1, 1, 0, 0, 0, 0, time.UTC)

	if got := dateAlignment(near, farFuture, DefaultDateWindow); got != 0 {
		t.Errorf("dateAlignment with an extreme gap = %v, want 0", got)
	}
	if got := dateAlignment(farFuture, near, DefaultDateWindow); got != 0 {
		t.Errorf("dateAlignment with an extreme gap (reversed) = %v, want 0", got)
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float64
		want float64
	}{
		{"identical vectors", []float64{1, 2, 3}, []float64{1, 2, 3}, 1},
		{"orthogonal vectors", []float64{1, 0}, []float64{0, 1}, 0},
		{"opposite vectors", []float64{1, 0}, []float64{-1, 0}, -1},
		{"zero vector", []float64{0, 0}, []float64{1, 1}, 0},
		{"mismatched length", []float64{1, 2}, []float64{1}, 0},
		{"empty vectors", nil, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("cosineSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}
