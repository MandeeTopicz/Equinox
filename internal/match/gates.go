package match

// GateResult is the outcome of one deterministic equivalence gate — a hard
// pass/reject check that runs before scoring, not a signal that gets
// blended with others. See docs/EQUIVALENCE.md's "deterministic gates"
// section.
type GateResult struct {
	Passed bool
	Reason string // populated when Passed is false
}
