# Project Equinox — PRD

## Problem

Prediction markets for the same real-world event are listed differently across venues — different names, expiration logic, contract design, and pricing formats. There is no way to programmatically detect that two listings refer to the same event, or to compare their pricing on a common basis. That fragmentation blocks cross-venue comparison and any form of intelligent routing.

## Objective

Build a prototype that:

- Connects to multiple prediction market venues via public APIs
- Normalizes each venue's markets into one canonical internal model
- Detects when markets across venues represent the same real-world event
- Simulates a routing decision for a hypothetical order across matched venues
- Logs and explains both the matching and routing decisions

This is an infrastructure feasibility exercise, not a trading product. It is evaluated on architectural thinking, tradeoff awareness, and clarity of reasoning — not production polish or UI.

## In scope

- Ingesting market metadata and pricing from public APIs of 3 venues (Polymarket, Kalshi, Manifold) — 2 is the stated minimum; a third is included to prove the architecture generalizes rather than being two special cases (see [DECISIONS.md](DECISIONS.md))
- A canonical, venue-agnostic market representation
- Equivalence detection across venues, with a documented and justified methodology (see [EQUIVALENCE.md](EQUIVALENCE.md))
- A routing engine that evaluates matched venues for a hypothetical order and explains its choice (see [ROUTING.md](ROUTING.md))
- Graceful handling of incomplete or ambiguous data, with assumptions documented inline

## Explicitly out of scope

- Real-money trading, wallet integration
- Regulatory or compliance implementation
- Production UI — UI polish is not evaluated
- Execution-quality optimization (real order-book depth, slippage modeling, split execution) beyond a simple liquidity proxy

## Derived requirements

1. Venue integration, normalization, equivalence detection, and routing are separate, independently invokable layers.
2. Routing logic contains no venue-specific branching or assumptions — it only reads canonical fields.
3. "Equivalent" is explicitly defined, with a methodology justified in writing.
4. Routing decisions are explainable — not just an output, but a logged rationale.
5. All decisions (matches and routes) are recorded as an append-only, timestamped trail, kept separate from the market data used to produce them, so a decision stays explainable even after that data is refreshed.
6. The system degrades gracefully on missing or ambiguous venue data instead of failing.
7. Any AI usage is disclosed, with reasoning for where and why it's applied.

## Deliverables (per project brief)

| Deliverable | Document |
|---|---|
| Working prototype | source code (CLI) |
| Setup instructions | [README.md](../README.md) |
| Architecture overview | [ARCHITECTURE.md](ARCHITECTURE.md) |
| Equivalence logic explanation | [EQUIVALENCE.md](EQUIVALENCE.md) |
| Routing logic explanation | [ROUTING.md](ROUTING.md) |
| AI usage disclosure | [AI_USAGE.md](AI_USAGE.md) |

Tooling and infrastructure decisions (language, storage, venue selection, CLI design) and their tradeoffs are recorded in [DECISIONS.md](DECISIONS.md).
