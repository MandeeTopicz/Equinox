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

## Embedding provider: OpenAI (`text-embedding-3-small`)

**Context.** The equivalence matcher's composite score depends on a semantic similarity signal (60% of the score, see EQUIVALENCE.md), which requires an embedding model. The Language & Runtime decision above already established that this calls out to an external API rather than running in-process — it did not specify a provider.

**Decision.** OpenAI's `text-embedding-3-small`, called via a plain HTTP POST, authenticated through an `OPENAI_API_KEY` environment variable referenced by `api_key_env` in `equinox.yaml` — the same pattern already used for Kalshi.

**Alternatives considered.**
- *Voyage AI* — comparably cheap and equally low-friction to integrate, but its models are tuned and benchmarked primarily for retrieval (matching a query against a large document corpus), the core operation behind RAG systems. Equivalence detection here is pairwise comparison between two short, already-prefiltered titles — closer to semantic textual similarity (STS) than retrieval. OpenAI's models are benchmarked directly on STS, a closer match to this task. Given there's no labeled dataset to empirically validate either choice against (see EQUIVALENCE.md), the more standard, better-documented option is also the easier one to defend in writing.
- *Google Vertex AI embeddings* — would echo the brief's Go+GCP stack hint, but requires GCP project/service-account setup, reintroducing exactly the setup friction the pure-Go SQLite driver was chosen to avoid.
- *Local/open-weight model* — zero API cost and fully offline, but reintroduces the local-ML-ecosystem gap that justified "calls out to an API" in the first place; Go has no strong local embedding tooling.
- *Anthropic* — not applicable. Claude models are generative, not embedding models, and Anthropic has no first-party embeddings API. (Anthropic's own guidance for retrieval use cases points third parties to Voyage AI, which is covered above.)

**Consequences.** Lowest setup friction of any hosted option (one API key, one REST call, no SDK dependency), negligible cost at this data volume, and no explanation burden in the writeup since it's the most standard choice available. The tradeoff is a third external dependency (OpenAI, alongside the Go/GCP-adjacent stack) with no direct connection to the project's stated internal stack — accepted because task fit and setup simplicity outweighed stack-consistency here.

---

## Storage: SQLite

**Context.** The system needs a canonical market model and an auditable decision trail (see ARCHITECTURE.md's state vs. events split). The brief explicitly deprioritizes infrastructure sophistication in favor of clarity.

**Decision.** SQLite, via the pure-Go driver (`modernc.org/sqlite`), not the cgo-based `mattn/go-sqlite3`.

**Alternatives considered.**
- *In-memory only* — simplest possible option, zero persistence complexity. Rejected because it collapses the pipeline into one opaque run; there'd be nothing to inspect between `fetch`, `match`, and `route`, undermining the "layers are observable" goal.
- *Postgres / Cloud SQL* — unjustifiable at this scope. Standing up a real database server reads as effort spent exactly where the brief says not to ("infrastructure sophistication" is explicitly not the goal).

**Consequences.** SQLite is free and has zero hosting cost — it's an embedded file, not a server. The one real tradeoff is driver choice: the standard `mattn/go-sqlite3` requires cgo, meaning anyone running the prototype needs a C toolchain installed. `modernc.org/sqlite` is a pure-Go port — marginally slower, irrelevant at this data volume — that makes `go build` work on a clean machine with zero setup friction. Given ease of evaluation is a real concern, that tradeoff is worth taking.

One consequence discovered during implementation: `modernc.org/sqlite`'s current release requires Go 1.25+, which bumped this project's minimum from the originally stated Go 1.21+ (see README.md). This doesn't reintroduce the friction the driver choice was meant to avoid — `go build` auto-selects the newer toolchain via `GOTOOLCHAIN=auto` (the Go default since 1.21) with no manual install step — but it's noted here since it changes a previously stated prerequisite.

---

## Venue selection: Polymarket, Kalshi, Manifold (not PredictIt)

**Context.** The brief requires a minimum of two venues with public APIs.

**Decision.** Integrate three: Polymarket, Kalshi, and Manifold Markets.

**Alternatives considered.**
- *Exactly two (Polymarket + Kalshi)* — satisfies the stated minimum and is the more "on-genre" pair (Kalshi is a regulated venue with a company-style API, closer to what a reviewer from this kind of stack would expect). Would have been a fully sufficient answer.
- *PredictIt* — rejected outright. Its public API is thin and unofficial, a poor foundation for a prototype meant to demonstrate reliable integration.

**Why three anyway.** The core architectural claim under test is "routing logic contains no venue-specific assumptions." With exactly two venues, that claim is unverifiable — a reviewer can't distinguish a real abstraction from two hardcoded paths wearing an interface. Manifold has no auth requirement, making it a low-cost third integration, and its presence is the cheapest available proof that the `VenueClient` interface and routing engine generalize rather than being built around two known cases. It also gives equivalence detection a genuine 3-way match group to demonstrate, rather than only pairs.

**Consequences.** Slightly more integration surface and a marginally harder matching problem (grouping across three sources instead of two), in exchange for a structural claim the project can actually back up rather than assert.

**Kalshi auth, discovered during implementation.** Kalshi's real authentication scheme is RSA-PSS request signing (a Key ID plus a private key, with `KALSHI-ACCESS-KEY`/`KALSHI-ACCESS-SIGNATURE`/`KALSHI-ACCESS-TIMESTAMP` headers computed per request) — not a bearer token that fits the `api_key_env` pattern used for OpenAI. Separately, Kalshi's read-only market data endpoint (`GET /trade-api/v2/markets`) works fully unauthenticated. Since `fetch` only ever reads market data, the Kalshi client uses no authentication at all rather than implementing RSA-PSS signing for endpoints this prototype doesn't call — consistent with the project's low-setup-friction bias elsewhere (SQLite driver, embedding provider), and it means Kalshi needs no API key, same as Manifold. `KALSHI_API_KEY` was removed from `.env.example`, `equinox.yaml`, and CLAUDE.md's required-env-vars list accordingly. (Also discovered in the same pass: the originally documented base URL, `trading-api.kalshi.com`, has been retired in favor of `api.elections.kalshi.com` — updated in `equinox.yaml` and README.)

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

---

## Fetch coverage: Kalshi priority series

**Context.** Each venue client's single-page (or small-bounded-page) fetch is a deliberate scoping assumption (see ARCHITECTURE.md). In practice this produced an unrepresentative sample: Polymarket's default ordering happened to return 98 near-duplicate "who wins the 2028 election" markets out of 100, and Kalshi's fixed default event order placed a real, live, high-liquidity Fed rate decision event (`KXFED-26SEP`) at position #5552 — page 28 of the events listing.

**Decision.** Two different fixes for two different problems. Polymarket: order `/markets` by 24h volume descending — the API supports this directly, it's a single request (no added cost), and volume is a reasonable proxy for "likely to have a real cross-venue counterpart" (thin markets are unlikely to be independently listed on multiple platforms). Kalshi: bounded pagination (5 pages) *plus* a small, explicitly disclosed list of series tickers (currently just `KXFED`) fetched via the `series_ticker` filter, on top of the general pages.

**Alternatives considered.**
- *Kalshi: sort/filter by activity, matching the Polymarket fix* — tried `sort`, `order_by`, `min_volume`, `min_liquidity`, `max_close_ts`, and `category` as query params against the real API; every one was silently ignored, returning the identical fixed default order regardless. Kalshi's `/events` endpoint has no such lever. Not available, not a choice made against it.
- *Kalshi: push bounded pagination deep enough to reach any specific event* — technically simple, but `KXFED-26SEP` sitting at page 28 means "deep enough" isn't actually bounded in any meaningful sense, and there's no guarantee the next relevant event isn't at page 50. Rejected as abandoning the "still bounded" framing in substance while keeping it in name.
- *No targeted list at all, general pagination only* — the more general, principled choice, and free of any risk of curve-fitting the sample toward a desired demo outcome. Rejected because it doesn't actually solve the coverage problem it was meant to solve — whether any specific well-known topic is captured stays down to luck within an arbitrary first slice, just a wider one.

**Consequences.** The series list is a real, if narrow, exception to "no venue-specific tuning of what gets fetched." Kept deliberately small and justified per entry (`KXFED` — independently verified as a real, live topic with a genuine, confirmed Polymarket counterpart, not chosen because it happened to produce a nice demo result) specifically to stay on the right side of "disclosed, principled targeting of a topic known to recur across major venues" rather than "curated to guarantee an outcome." Growing this list casually in the future would erode that distinction; any addition should meet the same bar — independently verified as a persistently cross-venue-common topic, not reverse-engineered from a specific run's output.

---

## Equivalence gates: extraction, not judgment — and why full LLM-judged equivalence was set aside

**Context.** Wider fetch coverage (above) surfaced a real false-positive pattern: markets with different numeric thresholds or different named subjects, sharing enough topical vocabulary and date proximity to clear the composite score anyway (see EQUIVALENCE.md's Known Limitations — the S&P 500 threshold-ladder and multi-candidate-vs-single-candidate cases). Embedding cosine similarity measures topical closeness; it has no mechanism to represent "these resolve differently in scenario X." Fixing that needed either a smarter deterministic check, or something that could actually reason about resolution criteria.

**Decision.** Two gates ahead of scoring, each a hard reject with no score to outweigh it. Numeric thresholds: plain regex, no AI — cheap and the pattern is genuinely mechanical (extract magnitude+unit tokens, compare as sets). Named entities: a narrow LLM call that only extracts — "list the entities named here" — never one that judges equivalence. The actual equivalence decision (do the two extracted sets match) stays a plain, logged rule.

**Alternatives considered.**
- *Ask an LLM directly whether two markets are equivalent* — the most accurate option of those considered, since it could reason about resolution-criteria logic directly rather than through a fixed set of extraction rules, and it was seriously considered. Set aside for two reasons. First, auditability: "list the entities in this text" produces an output a reader can independently verify against the source (does that name actually appear?); "are these equivalent, yes/no" produces a verdict that can only be trusted or not — there's nothing in the log to check it against. Second, consistency with this project's existing AI boundary: routing is deliberately kept model-free for determinism and auditability (AI_USAGE.md), and the equivalence matcher's one existing AI touchpoint (embedding similarity) is a narrow, bounded signal feeding a deterministic formula, not a black-box verdict — a direct equivalence judgment would be a materially different kind of AI usage than anything else in this codebase, not just a bigger version of the same thing.
- *Named-entity extraction via regex/heuristics, matching the threshold gate's approach* — tried conceptually and rejected before implementation: proper-noun detection by capitalization is unreliable on market titles, which capitalize plenty of non-entity words ("Will", "Democratic", "AGI"), producing both false positives and false negatives with no clean fix. This is the genuine reason an LLM call is used for this gate and not the other.
- *No gates at all, rely on retuning `--min-score` instead* — rejected outright per the discussion that led here: a compensatory weighted score has no threshold that's simultaneously permissive enough to catch genuine paraphrases and strict enough to reject every structurally-different-but-topically-close pair: moving the cutoff changes which such pairs get through, not whether the failure mode exists.

**Consequences.** The gates catch the two failure patterns actually found in real data; they are not a comprehensive logical-equivalence checker and don't claim to be — a structural difference expressed some other way (contest type, conditional structure, "by" vs. "at" a date) would still pass both gates undetected. Extending gate coverage as new patterns are found, rather than reaching for a general LLM-judgment verdict, is the intended way this narrows over time, trading broader one-shot coverage for verifiability, consistent with this project's precision-over-recall bias (EQUIVALENCE.md). The entity extractor is a second real OpenAI dependency (`gpt-4o-mini` chat completions, alongside the `text-embedding-3-small` embedding calls) — disclosed in AI_USAGE.md — and, verified directly during development, meaningfully slower at scale than the embedding-only pipeline was, since extraction runs one sequential HTTP call per unique candidate market rather than one batched call for all of them (see EQUIVALENCE.md's Known Limitations). Acceptable for a prototype's occasional `match` run; a production system would need to batch or parallelize this.
