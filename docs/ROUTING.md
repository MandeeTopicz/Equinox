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

If `--event` refers to a market with no match group (nothing cleared the equivalence threshold against it), routing has nothing to compare against; this is logged as a single-venue no-op decision rather than treated as an error.

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
