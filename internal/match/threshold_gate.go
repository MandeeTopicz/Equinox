package match

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"equinox/internal/normalize"
)

// percentBpsPattern matches a percentage or basis-point magnitude, with the
// number and unit captured separately so they can be reconstructed into a
// canonical form — "50bps", "50 bps", and "50 basis points" all mean the
// same threshold and must normalize identically.
var percentBpsPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(%|percent|bps?|basis\s+points?)`)

// dollarPattern matches a dollar amount, with an optional k/m/b/thousand/
// million/billion magnitude suffix. \b after the suffix group is required:
// without it, "$100,000 by 2027" matches the "b" in "by" as a "billion"
// suffix, silently absorbing part of the next word.
var dollarPattern = regexp.MustCompile(`\$\s?([\d,]+(?:\.\d+)?)\s*(k|m|b|thousand|million|billion)?\b`)

// boundedThresholdPattern matches a bare number tied to inequality
// language (above/below/between/etc). Without that context, a bare number
// is as likely to be a year as a threshold, e.g. "2028" in "2028
// election" — so a number alone is never extracted.
var boundedThresholdPattern = regexp.MustCompile(`(?i)(above|below|over|under|exceeds?|at least|at most|between)\s+[\d,]+(?:\.\d+)?(?:\s+and\s+[\d,]+(?:\.\d+)?)?`)

// percentBpsUnitAliases maps a matched unit spelling to one canonical form,
// so equivalent phrasings ("percent" and "%", "bps" and "basis points")
// compare equal.
var percentBpsUnitAliases = map[string]string{
	"%":            "%",
	"percent":      "%",
	"bp":           "bps",
	"bps":          "bps",
	"basis point":  "bps",
	"basis points": "bps",
}

// extractThresholds returns the set of normalized threshold phrases found
// in text. Each is a literal (canonicalized) token, not a decomposed
// numeric value — "between 7000 and 7199.99" and "above 7000" stay
// distinct tokens even though "7000" appears in both, which is exactly the
// distinction that matters here (a range and an open-ended threshold are
// different propositions). This is deliberately a literal-phrase gate, not
// an attempt at numeric range reasoning.
func extractThresholds(text string) map[string]bool {
	found := map[string]bool{}

	for _, m := range percentBpsPattern.FindAllStringSubmatch(text, -1) {
		number, unit := m[1], strings.ToLower(strings.Join(strings.Fields(m[2]), " "))
		found[number+percentBpsUnitAliases[unit]] = true
	}

	for _, m := range dollarPattern.FindAllStringSubmatch(text, -1) {
		amount, suffix := m[1], strings.ToLower(m[2])
		found["$"+amount+suffix] = true
	}

	for _, match := range boundedThresholdPattern.FindAllString(text, -1) {
		normalized := strings.Join(strings.Fields(strings.ToLower(match)), " ")
		found[normalized] = true
	}

	return found
}

// ThresholdGate rejects a candidate pair whose titles specify different
// numeric thresholds, or where only one side specifies one at all — a
// discovered failure mode where a market matched many differently-thresholded
// sibling markets (e.g. 21 mutually exclusive "S&P 500 in range X" Kalshi
// contracts) via weak title similarity plus strong date alignment, at the
// documented default threshold, not just a lowered one (see
// docs/EQUIVALENCE.md). Runs before any scoring — this is a hard gate, not
// a signal that can be outweighed by others.
func ThresholdGate(a, b normalize.Market) GateResult {
	ta := extractThresholds(a.Title)
	tb := extractThresholds(b.Title)

	if len(ta) == 0 && len(tb) == 0 {
		return GateResult{Passed: true}
	}
	if setsEqual(ta, tb) {
		return GateResult{Passed: true}
	}

	return GateResult{
		Passed: false,
		Reason: fmt.Sprintf("threshold mismatch: %v vs %v", sortedKeys(ta), sortedKeys(tb)),
	}
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
