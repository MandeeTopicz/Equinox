package store

// schema defines the state and event tables (see docs/ARCHITECTURE.md).
//
// State tables (raw_markets, canonical_markets) are replaced wholesale per
// venue on every fetch. Event tables (match_decisions, routing_decisions)
// are append-only: every row is a frozen decision, never updated in place.
const schema = `
CREATE TABLE IF NOT EXISTS raw_markets (
	venue           TEXT NOT NULL,
	venue_market_id TEXT NOT NULL,
	raw_json        TEXT NOT NULL,
	fetched_at      TEXT NOT NULL,
	PRIMARY KEY (venue, venue_market_id)
);

CREATE TABLE IF NOT EXISTS canonical_markets (
	id              TEXT PRIMARY KEY,
	venue           TEXT NOT NULL,
	venue_market_id TEXT NOT NULL,
	title           TEXT NOT NULL,
	description     TEXT NOT NULL DEFAULT '',
	category        TEXT NOT NULL DEFAULT '',
	resolution_date TEXT NOT NULL,
	yes_price       REAL NOT NULL,
	no_price        REAL NOT NULL,
	liquidity       REAL NOT NULL,
	fetched_at      TEXT NOT NULL,
	UNIQUE (venue, venue_market_id)
);
CREATE INDEX IF NOT EXISTS idx_canonical_markets_venue ON canonical_markets(venue);

CREATE TABLE IF NOT EXISTS match_decisions (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id         TEXT NOT NULL,
	created_at       TEXT NOT NULL,
	min_score        REAL NOT NULL,
	score            REAL NOT NULL,
	title_similarity REAL NOT NULL,
	date_alignment   REAL NOT NULL,
	category_match   REAL NOT NULL,
	members_json     TEXT NOT NULL,
	signals_json     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_match_decisions_event ON match_decisions(event_id, created_at);

CREATE TABLE IF NOT EXISTS routing_decisions (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	match_decision_id INTEGER REFERENCES match_decisions(id),
	event_id          TEXT NOT NULL,
	side              TEXT NOT NULL,
	size              REAL NOT NULL,
	created_at        TEXT NOT NULL,
	selected_venue    TEXT,
	is_noop           INTEGER NOT NULL DEFAULT 0,
	rationale         TEXT NOT NULL,
	comparison_json   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_routing_decisions_event ON routing_decisions(event_id, created_at);
`
