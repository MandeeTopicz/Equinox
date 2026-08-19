# Decisions

Tooling and infrastructure choices for Project Equinox, and the tradeoffs behind each. Equivalence methodology is covered separately in [EQUIVALENCE.md](EQUIVALENCE.md); this file is scoped to language, storage, venue selection, and CLI/output design.

---

## Language & runtime: Go

**Context.** The brief states the target company's internal stack frequently uses Go and GCP, but leaves language choice open if justified.

**Decision.** Go.

**Alternatives considered.**
- *Python* — fastest to write, and the strongest ecosystem if equivalence detection leaned on local embeddings/NLP tooling (sentence-transformers, sklearn). Weaker on making architectural boundaries obvious at a glance; duck typing makes layering easy to blur without discipline.
- *TypeScript/Node* — reasonable middle ground for async fan-out across multiple venue APIs. No real ecosystem edge for this task's dominant complexity (equivalence scoring, layered architecture), and no stack-consistency edge either.

**Consequences.** Go's interfaces map directly onto the required layering (`VenueClient`, matcher, router as distinct interfaces), and the choice reads as intentional to reviewers already working in this stack. The tradeoff: no local ML ecosystem, so the embedding step in equivalence detection calls out to an API rather than running a model in-process.

---

## Storage: SQLite

**Context.** The system needs a canonical market model and an auditable decision trail (see ARCHITECTURE.md's state vs. events split). The brief explicitly deprioritizes infrastructure sophistication in favor of clarity.

**Decision.** SQLite, via the pure-Go driver (`modernc.org/sqlite`), not the cgo-based `mattn/go-sqlite3`.

**Alternatives considered.**
- *In-memory only* — simplest possible option, zero persistence complexity. Rejected because it collapses the pipeline into one opaque run; there'd be nothing to inspect between `fetch`, `match`, and `route`, undermining the "layers are observable" goal.
- *Postgres / Cloud SQL* — unjustifiable at this scope. Standing up a real database server reads as effort spent exactly where the brief says not to ("infrastructure sophistication" is explicitly not the goal).

**Consequences.** SQLite is free and has zero hosting cost — it's an embedded file, not a server. The one real tradeoff is driver choice: the standard `mattn/go-sqlite3` requires cgo, meaning anyone running the prototype needs a C toolchain installed. `modernc.org/sqlite` is a pure-Go port — marginally slower, irrelevant at this data volume — that makes `go build` work on a clean machine with zero setup friction. Given ease of evaluation is a real concern, that tradeoff is worth taking.

---

## Venue selection: Polymarket, Kalshi, Manifold (not PredictIt)

**Context.** The brief requires a minimum of two venues with public APIs.

**Decision.** Integrate three: Polymarket, Kalshi, and Manifold Markets.

**Alternatives considered.**
- *Exactly two (Polymarket + Kalshi)* — satisfies the stated minimum and is the more "on-genre" pair (Kalshi is a regulated venue with a company-style API, closer to what a reviewer from this kind of stack would expect). Would have been a fully sufficient answer.
- *PredictIt* — rejected outright. Its public API is thin and unofficial, a poor foundation for a prototype meant to demonstrate reliable integration.

**Why three anyway.** The core architectural claim under test is "routing logic contains no venue-specific assumptions." With exactly two venues, that claim is unverifiable — a reviewer can't distinguish a real abstraction from two hardcoded paths wearing an interface. Manifold has no auth requirement, making it a low-cost third integration, and its presence is the cheapest available proof that the `VenueClient` interface and routing engine generalize rather than being built around two known cases. It also gives equivalence detection a genuine 3-way match group to demonstrate, rather than only pairs.

**Consequences.** Slightly more integration surface and a marginally harder matching problem (grouping across three sources instead of two), in exchange for a structural claim the project can actually back up rather than assert.

---

## CLI & output design

**Context.** The brief asks for a working prototype with local deployment; UI polish is explicitly not evaluated. SQLite is the single source of truth (see above); the CLI needs to expose the pipeline without becoming a second source of truth itself.

**Decision.**
- Two command families: **pipeline** commands that mutate the database (`fetch`, `match`, `route`, `run`) and **view** commands that only read it (`show markets|matches|decisions`).
- Database tables split into **state** (`raw_markets`, `canonical_markets` — overwritten on each `fetch`) and **events** (`match_decisions`, `routing_decisions` — append-only).

**Alternatives considered.**
- *One monolithic `run` command with no sub-stages* — simpler surface, but makes the required layer separation something the reader has to take on faith from the code, rather than something they can observe by running each stage independently and inspecting the database in between.
- *A single mutable "current decision" record instead of an append-only log* — simpler schema, but re-running `fetch` (expected, since prices move) would silently invalidate the explanation behind a previous routing decision, since the data it was computed from would no longer match. Append-only avoids that: each decision stays explainable regardless of what the market data does afterward.

**Consequences.** `fetch`/`match`/`route` being separately invokable makes the four-layer architecture something a reviewer can verify by running commands and reading table contents, not just something asserted in prose. `run` exists purely so the whole thing is also demoable in one command, without requiring anyone to know the pipeline order first.
