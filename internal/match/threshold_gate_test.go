package match

import (
	"strings"
	"testing"
	"time"
)

func TestThresholdGate(t *testing.T) {
	tests := []struct {
		name       string
		titleA     string
		titleB     string
		wantPassed bool
	}{
		{
			name:       "neither side has a threshold",
			titleA:     "Will the Fed cut rates in March?",
			titleB:     "March FOMC: rate cut?",
			wantPassed: true,
		},
		{
			name:       "the canonical EQUIVALENCE.md example: one side has a threshold, the other doesn't",
			titleA:     "Will the Fed cut rates in March?",
			titleB:     "Will the Fed cut rates by 50bps in March?",
			wantPassed: false,
		},
		{
			name:       "same threshold, different phrasing",
			titleA:     "Will the Fed cut rates by 50bps in March?",
			titleB:     "50 bps rate cut in March?",
			wantPassed: true,
		},
		{
			name:       "different thresholds",
			titleA:     "Will the Fed cut rates by 25bps in March?",
			titleB:     "Will the Fed cut rates by 50bps in March?",
			wantPassed: false,
		},
		{
			name:       "percentages must match",
			titleA:     "Will inflation exceed 3% this year?",
			titleB:     "Will inflation exceed 4% this year?",
			wantPassed: false,
		},
		{
			name:       "dollar amounts must match",
			titleA:     "Will Bitcoin reach $100,000 by 2027?",
			titleB:     "Will Bitcoin reach $150,000 by 2027?",
			wantPassed: false,
		},
		{
			name:       "same dollar amount passes",
			titleA:     "Will Bitcoin reach $100,000 by 2027?",
			titleB:     "Bitcoin above $100,000 in 2027?",
			wantPassed: true,
		},
		{
			name:       "a bare year is not treated as a threshold",
			titleA:     "Will Trump win the 2028 election?",
			titleB:     "2028 presidential election: Trump wins?",
			wantPassed: true,
		},
		{
			name:       "a real S&P 500 ladder pair — different ranges, must reject",
			titleA:     "Will the S&P 500 be above 7000 on Dec 31, 2027 at 4pm EST?",
			titleB:     "Will the S&P 500 be above 8000 on Dec 31, 2027 at 4pm EST?",
			wantPassed: false,
		},
		{
			name:       "a real S&P 500 ladder pair — a range vs. an open threshold, must reject",
			titleA:     "Will the S&P 500 be between 7000 and 7199.99 on Dec 31, 2027 at 4pm EST?",
			titleB:     "Will the S&P 500 be above 7000 on Dec 31, 2027 at 4pm EST?",
			wantPassed: false,
		},
		{
			name:       "identical S&P 500 range phrased differently still matches",
			titleA:     "Will the S&P 500 be between 7000 and 7199.99 on Dec 31, 2027 at 4pm EST?",
			titleB:     "S&P 500 between 7000 and 7199.99 by end of 2027?",
			wantPassed: true,
		},
	}

	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := marketAt("polymarket", "", base)
			a.Title = tt.titleA
			b := marketAt("kalshi", "", base)
			b.Title = tt.titleB

			got := ThresholdGate(a, b)
			if got.Passed != tt.wantPassed {
				t.Errorf("ThresholdGate(%q, %q).Passed = %v, want %v (reason: %q)", tt.titleA, tt.titleB, got.Passed, tt.wantPassed, got.Reason)
			}
			if !tt.wantPassed && got.Reason == "" {
				t.Error("expected a non-empty rejection reason")
			}
			if !tt.wantPassed && !strings.Contains(got.Reason, "threshold mismatch") {
				t.Errorf("expected reason to mention threshold mismatch, got %q", got.Reason)
			}
		})
	}
}

func TestExtractThresholds(t *testing.T) {
	tests := []struct {
		text      string
		wantEmpty bool
	}{
		{"Will the Fed cut rates by 50bps in March?", false},
		{"Will inflation exceed 3.5%?", false},
		{"Will Bitcoin reach $100,000?", false},
		{"Will the 2028 election happen on time?", true},
		{"Will Trump win the 2028 election?", true},
	}
	for _, tt := range tests {
		got := extractThresholds(tt.text)
		gotEmpty := len(got) == 0
		if gotEmpty != tt.wantEmpty {
			t.Errorf("extractThresholds(%q) = %v, want empty=%v", tt.text, sortedKeys(got), tt.wantEmpty)
		}
	}
}
