# Architecture Overview

## Layering

The system is organized into four layers that mirror the project's core requirement: venue integration, normalization, equivalence detection, and routing must be separable, and routing must not know anything venue-specific. Each layer depends on the layer below it only through an interface, never a concrete venue implementation.

```
Venue Clients   ->   Normalization   ->   Equivalence Detection   ->   Routing
(Polymarket,          (canonical           (hybrid matcher:            (venue-agnostic
 Kalshi,               Market model)        score + signals)            decision + rationale)
 Manifold)
```

- **Venue Clients** — one implementation per venue behind a common `VenueClient` interface (fetch raw markets + pricing). All venue-specific parsing lives here and nowhere else.
- **Normalization** — maps each venue's raw payload into the canonical `Market` model. This is the only place venue schema knowledge is allowed to leak into a shared shape.
- **Equivalence Detection** — operates only on canonical markets, never raw venue payloads. Groups markets across venues believed to represent the same event, recording a score and the signals behind it. See [EQUIVALENCE.md](EQUIVALENCE.md).
- **Routing** — operates only on matched canonical market groups. Given a hypothetical order (event, side, size), evaluates each venue in the group and picks one, logging why. See [ROUTING.md](ROUTING.md).

Because routing only ever sees canonical `Market` structs and a match group, adding a fourth venue requires no routing code changes — that's the deliberate proof that the venue-agnostic requirement is real rather than asserted. (See [DECISIONS.md](DECISIONS.md) for why a third venue was added specifically to demonstrate this.)

## Data model: state vs. events

SQLite is the single source of truth; the CLI is a read/write client over it and nothing more. Tables split into two kinds:

| Kind | Tables | Behavior |
|---|---|---|
| **State** | `raw_markets`, `canonical_markets` | Overwritten/refreshed on every `fetch` — represents "what we currently know" |
| **Events** | `match_decisions`, `routing_decisions` | Append-only — each row is a frozen record of a decision and its reasoning at that moment |

This split exists because market data is expected to change (prices move, listings close) while a decision's justification must stay legible after the data that produced it has moved on. Without it, refreshing prices would silently invalidate the explanation behind a prior routing decision — the record would no longer match reality, making the audit trail meaningless. `routing_decisions` rows reference the `match_decisions` row they relied on, so a routing rationale can always be traced back to the equivalence rationale that justified treating those markets as one event.

## CLI as a view over the database

Two command families:

- **Pipeline** (mutate state/events): `fetch`, `match`, `route`, `run`
- **View** (read-only): `show markets`, `show matches`, `show decisions`

View commands never compute anything new — they only format rows that already exist. If a `show` command's output changes, something upstream (`fetch`/`match`/`route`) must have written new data.

```
equinox fetch                                      # venue APIs -> raw_markets, canonical_markets
equinox match [--min-score 0.75]                    # canonical_markets -> match_decisions
equinox route --event <id> --side yes --size 100    # match_decisions -> routing_decisions
equinox run                                         # fetch -> match -> route, one command

equinox show markets   [--venue X]                  # read canonical_markets
equinox show matches   [--event <id>]                # read match_decisions
equinox show decisions [--event <id>]                # read routing_decisions
```

Command semantics and defaults:

- `fetch` scopes to active/open markets by default — unbounded ingestion of every market a venue has ever listed isn't tractable for a prototype. This is a documented scoping assumption, not a hidden limitation: a production system would need true pagination/streaming to go further. Within that assumption, each venue client scopes its one bounded fetch differently, per venue-specific tradeoffs discovered during development (see `internal/venue`'s doc comments and [DECISIONS.md](DECISIONS.md)):
  - Polymarket: one page (its observed cap), ordered by 24h volume — the API's default order surfaced an unrepresentative single-topic slice during development, and volume also selects for markets most likely to have a real cross-venue counterpart.
  - Kalshi: a bounded handful of pages (still not unbounded) plus a small, disclosed list of specific known-relevant series fetched explicitly — Kalshi's `/events` endpoint has no relevance/activity sort or filter of any kind, so blind pagination alone cannot reliably reach a specific real event without an impractically deep page count.
  - Manifold: one page, already reasonably diverse without further scoping.
- `match --min-score` defaults to `0.75`; see [EQUIVALENCE.md](EQUIVALENCE.md) for how that number was chosen.
- `route --event` has no default. Routing an unspecified market is treated as a real ambiguity, not something to silently guess. `run` invoked without `--event` completes fetch+match and either stops with a pointer to the discovered match groups, or — for one-command demoability — auto-selects the highest-confidence group and explicitly logs that it did so (e.g. `no --event given, defaulting to highest-confidence match: <id>, score 0.91`).
- Any `show` command run before its upstream pipeline step has produced data returns an explicit "no data yet — run `equinox fetch`" style message rather than an error or empty silence.

## Output

Every match or routing decision is written to its event table with the inputs and signals that produced it, at the moment it's made — never reconstructed after the fact. `show` commands render these as a human-readable table by default, with a `--json` flag for machine-readable output.

## Planned project layout

```
cmd/equinox/            # CLI entrypoint, subcommand wiring
internal/venue/         # VenueClient interface + Polymarket/Kalshi/Manifold implementations
internal/normalize/     # raw payload -> canonical Market model
internal/match/         # equivalence detection (prefilter + composite scoring)
internal/route/         # routing engine
internal/store/         # SQLite schema, state vs. event table access
internal/cli/           # command handlers, table/JSON rendering
```
