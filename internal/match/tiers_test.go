package match

import "testing"

func TestClassifyTier(t *testing.T) {
	tests := []struct {
		name        string
		title, date float64
		want        Tier
	}{
		{"both comfortably above matched floors", 0.90, 0.95, TierMatched},
		{"exactly at matched floors (inclusive)", MatchedTitleFloor, MatchedDateFloor, TierMatched},
		{"strong date can't compensate for weak title", 0.30, 1.0, TierNone},
		{"strong title can't compensate for weak date", 1.0, 0.30, TierNone},
		{"both in the review band", 0.70, 0.75, TierNeedsReview},
		{"exactly at review floors (inclusive)", ReviewTitleFloor, ReviewDateFloor, TierNeedsReview},
		{"title clears matched but date only clears review", 0.85, 0.75, TierNeedsReview},
		{"date clears matched but title only clears review", 0.70, 0.95, TierNeedsReview},
		{"both below review floors", 0.40, 0.50, TierNone},
		{"title below review floor alone disqualifies despite strong date", 0.50, 0.95, TierNone},
		{"date below review floor alone disqualifies despite strong title", 0.95, 0.50, TierNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyTier(tt.title, tt.date); got != tt.want {
				t.Errorf("ClassifyTier(%v, %v) = %v, want %v", tt.title, tt.date, got, tt.want)
			}
		})
	}
}

func TestTierString(t *testing.T) {
	tests := []struct {
		tier Tier
		want string
	}{
		{TierMatched, "matched"},
		{TierNeedsReview, "needs review"},
		{TierNone, "none"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("Tier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}
