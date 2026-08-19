# Equivalence Logic

## Definition

Two markets from different venues are **equivalent** if they resolve based on the same real-world proposition — meaning a rational observer would expect both to settle the same way (both YES or both NO) based on the same underlying event, within the same resolution window — regardless of differences in title wording, contract structure, fee format, or venue-specific metadata.

This is a definition about the **proposition being resolved**, not surface similarity. "Will the Fed cut rates at the March 2026 FOMC meeting" and "March FOMC: rate cut?" are equivalent. "Will the Fed cut rates in March" and "Will the Fed cut rates by 50bps in March" are **not** — same topic, different resolution criteria, so a rational trader would not expect identical settlement in every scenario. Distinguishing topical relation from true equivalence is where naive text matching fails, and it's the main source of error this methodology has to manage.

## Design principle: precision over recall

A false negative (missing a real equivalence) leaves the system incomplete — a gap to fill later. A false positive (claiming two different markets are the same event) actively produces a wrong routing decision on top of it — a worse failure for infrastructure meant to be trusted. The methodology below is deliberately biased toward under-matching rather than over-matching.

## Methodology: hybrid, in two stages

**Stage 1 — heuristic prefilter.** Cheap, rule-based gates a candidate pair must clear before any similarity scoring runs:

- same category/tag, where venue metadata provides one
- resolution dates within a tolerance window (e.g. ±48h) — markets resolving weeks apart cannot be the same event, regardless of how similar their titles read

**Stage 2 — composite similarity score.** For pairs that survive the prefilter, a weighted score is computed:

| Signal | Weight | Why |
|---|---|---|
| Title/description semantic similarity (embedding cosine) | 0.60 | Catches paraphrased or reworded titles that pure string matching misses — the main gap rule-based matching alone leaves open |
| Resolution-date alignment (normalized closeness within the tolerance window) | 0.25 | Deterministic, cheap, and a strong disambiguating signal on its own |
| Category/tag match | 0.15 | Weak on its own (categories are coarse) but useful as a tie-breaker |

The composite score lands in `[0, 1]`. Every score is logged with the individual signal values that produced it — the rationale is the breakdown, not just the final number.

## Default threshold: `--min-score 0.75`

This is a reasoned starting point, not an empirically calibrated cutoff — there is no labeled dataset of known-equivalent cross-venue market pairs to validate against in a prototype setting, and that limitation is stated here deliberately rather than hidden. The reasoning: embedding cosine similarity on genuinely paraphrased same-topic text typically scores 0.80–0.95, while merely topically-adjacent-but-different text typically scores 0.50–0.70. `0.75` sits in that gap — closer to the permissive edge than a production system would want, but reasonable for a prototype where matches need to actually surface to be demonstrable. It is exposed as a flag specifically so it can be tuned without a code change, and any run of `equinox match` logs the threshold used alongside its results.

## Known limitations

- No labeled data to validate scoring weights or the threshold against — both are reasoned defaults, not fitted values.
- Two markets can be near-equivalent but structurally different (different thresholds, different sub-conditions) in a way that reads as high title similarity but is not truly equivalent — the resolution-date and category signals only partially guard against this.
- Only binary (yes/no) markets are matched against each other in this prototype; multi-outcome markets are out of scope.
