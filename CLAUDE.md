# Equinox — Claude Code Project Rules

## Project Overview
Equinox is a cross-venue prediction market aggregation and routing prototype. It pulls market data from three prediction market venues (Polymarket, Kalshi, Manifold), normalizes them into a shared internal representation, detects equivalent markets across venues, and simulates routing decisions for hypothetical trades. This is an infrastructure/architecture prototype, not a production trading system. Full context: [docs/PRD.md](docs/PRD.md).

## Tech Stack
- Language: Go. No hard-pinned version in this file — `README.md` states a `Go 1.21+` minimum, and `go.mod`'s `go` directive is the source of truth once the module exists.
- Storage: SQLite via `modernc.org/sqlite` (pure Go, no cgo — see [docs/DECISIONS.md](docs/DECISIONS.md)).
- Embeddings: OpenAI `text-embedding-3-small`, called via plain `net/http` — no SDK dependency (see [docs/DECISIONS.md](docs/DECISIONS.md)).
- Config: `equinox.yaml` (venues, match threshold, embedding provider, database path).
- CLI-based tool — no web frontend. UI polish is explicitly out of scope for this project.
- Keep dependencies minimal and justify any addition in [docs/DECISIONS.md](docs/DECISIONS.md) — this project has repeatedly favored the lower-friction option (see the SQLite driver and embedding provider decisions) and new dependencies should meet that bar.

## Directory Structure
This is the planned layout per [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md); no code exists yet as of this file's creation.

- `cmd/equinox` — CLI entrypoint, subcommand wiring
- `internal/venue` — `VenueClient` interface + Polymarket/Kalshi/Manifold implementations
- `internal/normalize` — canonical `Market` model, venue-agnostic normalization logic
- `internal/match` — equivalence detection (heuristic prefilter + composite scoring)
- `internal/route` — routing engine
- `internal/store` — SQLite schema and persistence (`raw_markets`, `canonical_markets` as state; `match_decisions`, `routing_decisions` as append-only events)
- `internal/cli` — command implementations (`fetch`, `match`, `route`, `run`, `show`)

Documentation lives in `docs/`, except the root `README.md`:
- `README.md` — setup instructions, config, CLI usage, example session
- `docs/PRD.md` — problem, objective, scope, derived requirements
- `docs/ARCHITECTURE.md` — layering, data model, CLI design
- `docs/EQUIVALENCE.md` — equivalence definition, hybrid scoring methodology
- `docs/ROUTING.md` — routing inputs, decision rule, rationale format
- `docs/AI_USAGE.md` — AI usage disclosure (runtime and development)
- `docs/DECISIONS.md` — language/storage/venue/embedding/CLI decisions and tradeoffs

Do not restructure these directories without flagging it first.

## Commands
- Build: `go build ./...`
- Run tests: `go test ./...`
- Run CLI: `go run ./cmd/equinox <command>`
- Format: `gofmt -w .`
- Vet: `go vet ./...`

Run tests and `go vet` before every commit. Do not commit code that fails either.

## Environment & Secrets
- Required env vars: `KALSHI_API_KEY`, `OPENAI_API_KEY` (referenced by name via `api_key_env` in `equinox.yaml`, never hardcoded). Manifold requires no auth — do not add unnecessary auth scaffolding for it.
- Loaded from a local `.env` file via a small loader (e.g. `godotenv`). A `.env.example` file must stay up to date with every required variable, using placeholder values only.
- Never commit `.env`, real API keys, or any credential value in any file, commit message, or PR description.

## Coding & Naming Conventions
- Standard Go naming conventions (exported identifiers PascalCase, unexported camelCase).
- Keep venue-specific logic isolated inside each venue's client implementation in `internal/venue` — code in `internal/normalize`, `internal/match`, and `internal/route` must stay venue-agnostic (no venue-specific branching outside the venue client layer). This is the project's core architectural claim; treat violations of it as bugs, not style issues.
- Document non-obvious design decisions inline or in the relevant `docs/` file, not just in commit messages.

## Definition of Done (per task/chunk)
A task is not complete until:
1. Code builds (`go build ./...`) with no errors.
2. Tests pass (`go test ./...`).
3. `go vet` is clean.
4. Relevant docs are updated if behavior changed — most often `docs/EQUIVALENCE.md` (scoring/methodology), `docs/ROUTING.md` (routing rule), `docs/ARCHITECTURE.md` (structure), or `docs/DECISIONS.md` (a new or changed tradeoff decision).
5. No secrets, keys, or credentials are present in any changed file.

## Git & GitHub Workflow
- Applies to code implementation from here forward: never commit directly to `main`. All code work happens on feature branches.
- Documentation may still be committed directly to `main` when explicitly directed, as has been the practice so far.
- One branch per logical chunk of work (not one per tiny task) — group related tasks into a single branch.
- Branch naming: `feat/<short-description>` (e.g. `feat/venue-clients`, `feat/equivalence-matcher`).
- After finishing a chunk: push the branch and open a PR against `main` using `gh pr create`.
- Do not merge the PR automatically — stop and let it be reviewed and merged manually, unless explicitly told to merge.
- Write clear, factual PR descriptions: what changed, why, and how it was tested. No filler.

## Commit Authorship — STRICT
- All commits must be authored under the repo owner's GitHub identity only.
- NEVER add a "Co-Authored-By: Claude" line or any Claude/Anthropic attribution to any commit message, trailer, or PR description.
- Do not include "Generated with Claude Code" or similar tags anywhere in commits or PRs.
- Commit messages should read as plain and factual, with no AI attribution of any kind.

## Commit Style
- Present tense, imperative mood ("Add config loader" not "Added" or "Adds").
- Keep each commit scoped to what it actually changed — no unrelated changes bundled in.

## Execution Loop Expectations
- When given a task, don't just write code — write it, run the build/tests, fix failures, and confirm it meets the Definition of Done above before reporting the task complete.
- Before starting any chunk of work, show the plan (files to touch, approach) and wait for confirmation before making changes — especially before anything destructive (schema changes, file deletions, force pushes).
- Break large tasks into smaller checkpoints rather than attempting an entire chunk in one uninterrupted pass.

## Files Claude Should Never Touch
- `.env` (real values)
- Anything under `.git/` directly (use git commands, not manual edits)
- CI/deployment secrets or credentials of any kind
