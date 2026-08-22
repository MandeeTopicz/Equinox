# Routing Logic

## What routing answers

Equivalence detection answers "are these the same market?" Routing answers a separate question: **given a matched group of equivalent markets and a hypothetical order, where would that order be executed, and why?** The two are kept deliberately distinct — see [EQUIVALENCE.md](EQUIVALENCE.md) for the matching question, and `match_decisions` vs. `routing_decisions` in [ARCHITECTURE.md](ARCHITECTURE.md) for how that separation is stored.

## Inputs

A routing decision requires:

- **`--event`** — the canonical (internal) match-group ID, not a venue-native ticker. This keeps the entrypoint itself venue-agnostic, and is discoverable via `equinox show matches`.
- **`--side`** — `yes` or `no`. Prediction contracts price each side independently across venues, so "best venue" is undefined without one.
- **`--size`** — the hypothetical contract count. A quoted top-of-book price is meaningless if a venue can't fill the requested size at it; size is what makes routing more than "read the lowest number off three APIs."

## What the engine evaluates

For every venue in the matched group, the router reads only canonical fields that exist identically across every venue implementation:

- normalized price for the requested side
- a liquidity proxy (available size at the quoted price, or venue-reported volume/liquidity metadata) — not a full order-book depth simulation

The router never branches on venue identity or name. It selects the venue with the best effective price for the requested side among venues whose liquidity proxy plausibly supports the requested size; ties are broken on higher liquidity. This is a deliberately simple, deterministic rule — the objective is demonstrating the reasoning and structure of a routing decision, not optimizing execution quality (see Out of scope, below).

If `--event` refers to a market with no match group (nothing cleared even the `needs review` tier against it), routing has nothing to compare against; this is logged as a single-venue no-op decision rather than treated as an error. Concretely: `route --event <id>` first looks up `<id>` as a match-group event id; if none exists, it falls back to treating `<id>` as a raw canonical market id (`venue:venue_market_id`, discoverable via `equinox show markets`) and, if that resolves, logs a no-op decision for that single venue — nothing selected, since there's nothing to compare against, just a record that routing was attempted. If neither resolves, `<id>` doesn't refer to anything real and that *is* an error.

## Tier gate: routing a "needs review" match

A match group's event id does resolve to something real, but that doesn't by itself mean routing should proceed unattended. Every match group carries a tier — `matched` or `needs review` (see [EQUIVALENCE.md](EQUIVALENCE.md)'s "Conjunctive tier floors") — derived from the same `title_similarity`/`date_alignment` values already stored on its `match_decisions` row, not a separate field.

- **`matched`** — `equinox route --event <id> ...` proceeds normally.
- **`needs review`** — `equinox route --event <id> ...` refuses by default:

  ```
  $ ./equinox route --event some-plausible-but-unconfirmed-pair --side yes --size 100
  error: event "some-plausible-but-unconfirmed-pair" is "needs review" (score 0.71), not "matched" — not confident enough to route automatically; re-run with --confirm-review to route anyway, acknowledging this hasn't cleared the matched threshold
  ```

  `--confirm-review` overrides this and routes anyway. The flag is a deliberate acknowledgment, not a silent bypass — it exists so that routing a lower-confidence match is a visible, opt-in decision by whoever ran the command, not something that happens by default because a pair happened to clear the lower of the two tiers. `equinox run`'s auto-select (no `--event` given) never picks a `needs review` group on its own; if the only groups found are `needs review`, `run` says so and points at `equinox show matches` / `equinox route --event <id> --confirm-review` rather than guessing.

This gate lives in the routing layer, not the matching layer, on purpose: equivalence detection's job is to classify confidence honestly (including the middle tier), and routing's job is to decide what to do with that classification — conflating the two would mean either silently downgrading `needs review` groups out of existence, or silently routing them as if they were `matched`. Keeping the refusal here, next to the rest of routing's own rules, keeps that decision visible and in one place.

## Rationale format

Each routing decision is logged as a comparison table across all venues in the match group, plus the selected venue and the reason it won — e.g.:

```
event: fed-march-2026-cut
side: yes, size: 100

venue       price   liquidity_ok   selected
kalshi      0.62    yes            yes  <- best price at requested size
polymarket  0.65    yes            no
manifold    0.60    no             no   <- insufficient liquidity at size 100

selected: kalshi — best YES price at requested size (0.62 vs. 0.65); manifold excluded on liquidity
```

This table is what gets written to `routing_decisions` — not just the winner, but every venue considered and why it did or didn't win.

## Out of scope

- Real order-book depth simulation
- Fee-adjusted execution modeling
- Multi-venue split execution
- Any optimization beyond the single deterministic price/liquidity rule above

These are documented simplifications, consistent with the project's stated priority: understanding the reasoning and structure behind a routing decision, not optimizing execution quality.
