# AI Usage Disclosure

## In the pipeline (runtime)

AI is used in exactly one place in the running system: the equivalence matcher's composite score includes a semantic text-similarity signal computed via an embedding model, applied to market titles/descriptions (see [EQUIVALENCE.md](EQUIVALENCE.md)). This exists to catch equivalent markets worded differently across venues — e.g. "Will the Fed cut rates in March" vs. "March FOMC rate decision" — which pure rule-based string and date matching would miss. It contributes 60% of the composite equivalence score; the remainder comes from deterministic date-window and category signals.

**Routing contains no AI.** The routing decision is a deterministic comparison over price, side, size, and a liquidity proxy across venues in an already-matched group (see [ROUTING.md](ROUTING.md)) — no model is called anywhere in that path. This separation is deliberate: equivalence is a fuzzy, language-shaped question well-suited to a similarity model; routing is a numeric comparison that doesn't need one, and keeping it model-free keeps that decision fully deterministic and auditable.

## In research and development

AI tools were used throughout the design and development of this project — for working through architectural tradeoffs, and for implementation assistance. This is disclosed at that level rather than broken down conversation-by-conversation or line-by-line; the runtime usage described above is the disclosure that materially affects how the system behaves, and is documented in full.
