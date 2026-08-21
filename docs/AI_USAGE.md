# AI Usage Disclosure

## In the pipeline (runtime)

AI is used in two places in the running system, both confined to equivalence detection — never routing.

**1. Title-similarity scoring.** The equivalence matcher's composite score includes a semantic text-similarity signal computed via OpenAI's `text-embedding-3-small` embedding model, applied to market titles/descriptions (see [EQUIVALENCE.md](EQUIVALENCE.md); provider selection reasoning is in [DECISIONS.md](DECISIONS.md)). This exists to catch equivalent markets worded differently across venues — e.g. "Will the Fed cut rates in March" vs. "March FOMC rate decision" — which pure rule-based string and date matching would miss. It contributes 60% of the composite equivalence score; the remainder comes from deterministic date-window and category signals.

**2. Named-entity extraction (a gate, not a score).** Before that scoring runs, a candidate pair must also clear a deterministic entity gate: the specific people or organizations named in each market's title are extracted via a narrow call to OpenAI's `gpt-4o-mini` chat completions model, and the pair is rejected outright if the two extracted sets differ (e.g. "Will Rubio win?" vs. "Will Vance, Rubio, or Newsom win?" name different people and resolve differently whenever a non-matching name wins, despite scoring highly on title similarity). This exists because embedding similarity measures topical closeness, not logical equivalence, and a real false-positive pattern was found in live data where that gap let mutually-exclusive markets score above the match threshold (see EQUIVALENCE.md's Known Limitations).

The model's job here is deliberately narrowed to *extraction*, not *judgment* — it is asked "what entities are named in this text," never "are these two markets equivalent." That distinction is the reason this gate is defensible as an audit trail: the extracted list is independently checkable by rereading the source title, where a bare equivalence verdict would not be. The actual accept/reject decision is a plain, logged set-comparison rule over the extracted output, not a model output itself. A companion deterministic gate (numeric threshold extraction, via plain regex) catches a related failure pattern without any model call at all. Full reasoning for why an LLM was used for entities specifically, and why a direct "are these equivalent" prompt was considered and rejected, is in [DECISIONS.md](DECISIONS.md).

**Routing contains no AI.** The routing decision is a deterministic comparison over price, side, size, and a liquidity proxy across venues in an already-matched group (see [ROUTING.md](ROUTING.md)) — no model is called anywhere in that path. This separation is deliberate: equivalence is a fuzzy, language-shaped question well-suited to AI assistance; routing is a numeric comparison that doesn't need it, and keeping it model-free keeps that decision fully deterministic and auditable.

## In research and development

AI tools were used throughout the design and development of this project — for working through architectural tradeoffs, and for implementation assistance. 
