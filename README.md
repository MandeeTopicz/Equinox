# Equinox

Cross-venue prediction market aggregation and routing simulation. A prototype that ingests markets from multiple prediction market venues, normalizes them into a shared internal representation, detects when markets across venues represent the same real-world event, and simulates routing decisions for hypothetical orders — logging and explaining every match and routing decision it makes.

This is an infrastructure feasibility prototype, not a trading product. See [docs/PRD.md](docs/PRD.md) for the full problem statement and scope.

## Docs

- [docs/PRD.md](docs/PRD.md) — problem, objective, scope
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — layers, data model, CLI overview
- [docs/EQUIVALENCE.md](docs/EQUIVALENCE.md) — what "equivalent" means and how it's detected
- [docs/ROUTING.md](docs/ROUTING.md) — how a routing decision is made and explained
- [docs/AI_USAGE.md](docs/AI_USAGE.md) — where AI is used, and where it deliberately isn't
- [docs/DECISIONS.md](docs/DECISIONS.md) — language, storage, venue, and CLI design tradeoffs

## Prerequisites

- Go 1.25+ (bumped from the originally targeted 1.21+ by the SQLite driver's toolchain requirement — see [DECISIONS.md](docs/DECISIONS.md))
- No database server required — storage is an embedded SQLite file (pure-Go driver, no cgo/C toolchain needed; see [DECISIONS.md](docs/DECISIONS.md))

## Configuration

Venues and their API base URLs live in `equinox.yaml` at the project root:

```yaml
venues:
  - name: polymarket
    base_url: https://gamma-api.polymarket.com
  - name: kalshi
    base_url: https://api.elections.kalshi.com
  - name: manifold
    base_url: https://api.manifold.markets

match:
  min_score: 0.75
  embedding:
    provider: openai
    model: text-embedding-3-small
    api_key_env: OPENAI_API_KEY

database:
  path: ./equinox.db
```

API keys, where required, are read from the environment variable named in `api_key_env` — never committed to the config file. See [docs/DECISIONS.md](docs/DECISIONS.md) for why OpenAI was chosen as the embedding provider.

## Build

```
go build -o equinox ./cmd/equinox
```

## Usage

Pipeline commands (write to the database):

```
./equinox fetch                                     # ingest markets from all configured venues
./equinox match                                      # detect cross-venue equivalence, default --min-score 0.75
./equinox route --event <id> --side yes --size 100   # simulate a hypothetical order
./equinox run                                        # fetch -> match -> route in one step
```

View commands (read-only, safe to run anytime — return "no data yet" if the pipeline hasn't run):

```
./equinox show markets   [--venue polymarket]
./equinox show matches   [--event <id>]
./equinox show decisions [--event <id>]
```

## Example session

```
$ ./equinox fetch
fetched 340 markets from polymarket, 128 from kalshi, 512 from manifold

$ ./equinox match
matched 41 cross-venue groups (min-score 0.75)

$ ./equinox show matches
event                    venues                        score
fed-march-2026-cut       polymarket, kalshi, manifold   0.91
...

$ ./equinox route --event fed-march-2026-cut --side yes --size 100
selected: kalshi — best YES price at requested size (0.62 vs. polymarket 0.65); manifold excluded on liquidity

$ ./equinox show decisions --event fed-march-2026-cut
[full comparison table + rationale, as recorded in routing_decisions]
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for command defaults, ambiguity handling, and the reasoning behind the pipeline/view split.
