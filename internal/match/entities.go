package match

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"equinox/internal/normalize"
)

// EntityExtractor lists the named entities (people, organizations) a
// market's outcome depends on, given its title. It extracts facts from
// text — it never judges whether two markets are equivalent. That
// distinction is deliberate: the output is independently checkable against
// the source text (does this entity actually appear in the title?), unlike
// a bare equivalence verdict. See docs/AI_USAGE.md.
type EntityExtractor interface {
	ExtractEntities(ctx context.Context, text string) ([]string, error)
}

// EntityGate rejects a candidate pair whose titles name different sets of
// entities — e.g. "Will Marco Rubio win the 2028 election?" against "Will
// JD Vance, Marco Rubio OR Gavin Newsom win the 2028 election?" have
// different resolution criteria (the second resolves YES if Vance wins;
// the first doesn't) even though they share most of their wording and
// resolve on the same date. A market naming no specific entity (most
// markets: dates, prices, general events) always passes — this gate only
// fires when at least one side actually names someone.
func EntityGate(ctx context.Context, a, b normalize.Market, extractor EntityExtractor) (GateResult, error) {
	ea, err := extractor.ExtractEntities(ctx, a.Title)
	if err != nil {
		return GateResult{}, fmt.Errorf("extracting entities from %q: %w", a.Title, err)
	}
	eb, err := extractor.ExtractEntities(ctx, b.Title)
	if err != nil {
		return GateResult{}, fmt.Errorf("extracting entities from %q: %w", b.Title, err)
	}
	return entityGateFromSets(ea, eb), nil
}

// entityGateFromSets is EntityGate's comparison logic, factored out so
// Match can reuse it against entities extracted once per unique candidate
// market rather than once per pair.
func entityGateFromSets(ea, eb []string) GateResult {
	if len(ea) == 0 && len(eb) == 0 {
		return GateResult{Passed: true}
	}
	if entitySetsEqual(ea, eb) {
		return GateResult{Passed: true}
	}
	return GateResult{
		Passed: false,
		Reason: fmt.Sprintf("entity sets differ: %v vs %v", ea, eb),
	}
}

func entitySetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, e := range a {
		set[strings.ToLower(strings.TrimSpace(e))] = true
	}
	for _, e := range b {
		if !set[strings.ToLower(strings.TrimSpace(e))] {
			return false
		}
	}
	return true
}

// OpenAIEntityExtractor calls OpenAI's chat completions API directly over
// HTTP — no SDK, consistent with OpenAIEmbeddingClient. One request per
// text (not batched into a single prompt): simpler and more reliable than
// relying on a chat model to preserve strict ordering across many inputs
// in one JSON response, at the cost of more requests. Extraction only
// happens for markets in a candidate pair that already survived the
// prefilter, so volume stays bounded.
type OpenAIEntityExtractor struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client

	// maxRetries and retryBaseDelay govern retry-with-backoff on HTTP 429
	// (rate limited) responses only — every other non-2xx status fails
	// immediately, since 429 is the one response OpenAI sends specifically
	// to say "slow down", not "something is wrong". A defensive backstop
	// for occasional overshoot, not the primary rate-limiting mechanism —
	// see limiter below for why per-call reactive backoff alone isn't
	// enough. Discovered live: once extraction ran concurrently (see
	// entityExtractionConcurrency in matcher.go), a real match run against
	// real fetch-coverage volume hit the account's actual gpt-4o-mini rate
	// ceiling (500 requests/min) and, before this existed, failed the
	// entire run — losing all extraction work already done — rather than
	// backing off. See docs/DECISIONS.md.
	maxRetries     int
	retryBaseDelay time.Duration

	// limiter paces requests to stay under the account's rate ceiling
	// proactively, rather than relying solely on retryBaseDelay to react
	// after the fact. Verified live that retry-with-backoff alone isn't
	// enough: with entityExtractionConcurrency workers all issuing
	// requests as fast as they can, once the account-wide ceiling is hit
	// it stays hit — every worker keeps tripping 429 simultaneously for
	// as long as aggregate demand exceeds the sustainable rate, which for
	// a large match run is minutes, not the few seconds maxRetries's
	// backoff schedule is sized for. A shared limiter caps how often any
	// worker starts a new request in the first place. See
	// docs/DECISIONS.md.
	limiter *rateLimiter
}

// entityExtractionRateLimitPerMinute paces entity extraction requests
// safely under the observed account ceiling for gpt-4o-mini (500
// requests/min) — reasoned headroom, not the literal limit, since other
// usage may share the same account and the exact ceiling can vary by
// account/tier. See docs/DECISIONS.md.
const entityExtractionRateLimitPerMinute = 450

// NewOpenAIEntityExtractor builds an extractor for the given chat model
// (e.g. gpt-4o-mini), authenticated with apiKey. A nil httpClient uses
// http.DefaultClient.
func NewOpenAIEntityExtractor(apiKey, model string, httpClient *http.Client) *OpenAIEntityExtractor {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIEntityExtractor{
		apiKey:         apiKey,
		model:          model,
		baseURL:        "https://api.openai.com",
		httpClient:     httpClient,
		maxRetries:     5,
		retryBaseDelay: time.Second,
		limiter:        newRateLimiter(entityExtractionRateLimitPerMinute),
	}
}

// rateLimiter paces calls to at most perMinute per minute, shared across
// however many concurrent callers use it (see entityExtractionConcurrency
// in matcher.go) — a simple virtual-scheduler token bucket: each caller
// reserves the next available slot under a mutex, then sleeps until it
// arrives, so the aggregate rate at which new requests start never
// exceeds perMinute regardless of concurrency. perMinute <= 0 disables
// limiting entirely (used in tests, where a handful of local httptest
// calls have no real rate ceiling to respect).
type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newRateLimiter(perMinute int) *rateLimiter {
	if perMinute <= 0 {
		return &rateLimiter{}
	}
	return &rateLimiter{interval: time.Minute / time.Duration(perMinute)}
}

func (rl *rateLimiter) wait(ctx context.Context) error {
	if rl == nil || rl.interval <= 0 {
		return nil
	}

	rl.mu.Lock()
	now := time.Now()
	if rl.next.Before(now) {
		rl.next = now
	}
	delay := rl.next.Sub(now)
	rl.next = rl.next.Add(rl.interval)
	rl.mu.Unlock()

	if delay <= 0 {
		return nil
	}
	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

const entityExtractionSystemPrompt = `You extract named entities (specific people or organizations) whose identity determines a prediction market's outcome.

Given a market question, respond with ONLY a JSON object of the form {"entities": ["Name1", "Name2"]}.

List each named person or organization explicitly mentioned as a subject of the proposition, using their name as written in the text. If the outcome does not depend on any specific named person or organization (e.g. it concerns a date, a price or numeric threshold, or a general event with no distinguishing individual), respond with {"entities": []}.

Do not infer or add entities not explicitly named in the text.`

type openAIChatRequest struct {
	Model          string               `json:"model"`
	Messages       []openAIChatMessage  `json:"messages"`
	ResponseFormat openAIResponseFormat `json:"response_format"`
	Temperature    float64              `json:"temperature"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type string `json:"type"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type entityExtractionResult struct {
	Entities []string `json:"entities"`
}

// ExtractEntities retries on a 429 (rate limited) response with
// exponential backoff (retryBaseDelay * 2^attempt), up to maxRetries
// times, before giving up. Every other error — including a non-429
// non-200 status — fails immediately, since retrying those wouldn't help.
func (c *OpenAIEntityExtractor) ExtractEntities(ctx context.Context, text string) ([]string, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		entities, retryable, err := c.extractEntitiesOnce(ctx, text)
		if err == nil {
			return entities, nil
		}
		lastErr = err
		if !retryable || attempt == c.maxRetries {
			break
		}
		delay := c.retryBaseDelay * time.Duration(1<<attempt)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// extractEntitiesOnce makes a single attempt, first waiting for the
// limiter to admit it. retryable is true only for a 429 response — the
// signal OpenAI uses specifically to mean "you're going too fast", not
// that anything is actually wrong with the request.
func (c *OpenAIEntityExtractor) extractEntitiesOnce(ctx context.Context, text string) (entities []string, retryable bool, err error) {
	if err := c.limiter.wait(ctx); err != nil {
		return nil, false, err
	}

	reqBody, err := json.Marshal(openAIChatRequest{
		Model: c.model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: entityExtractionSystemPrompt},
			{Role: "user", Content: text},
		},
		ResponseFormat: openAIResponseFormat{Type: "json_object"},
		Temperature:    0,
	})
	if err != nil {
		return nil, false, fmt.Errorf("marshaling entity extraction request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, false, fmt.Errorf("building entity extraction request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("calling OpenAI chat completions API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, resp.StatusCode == http.StatusTooManyRequests, fmt.Errorf("OpenAI chat completions API returned %s: %s", resp.Status, string(body))
	}

	var parsed openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, false, fmt.Errorf("decoding chat completion response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, false, fmt.Errorf("chat completion response had no choices")
	}

	var result entityExtractionResult
	if err := json.Unmarshal([]byte(parsed.Choices[0].Message.Content), &result); err != nil {
		return nil, false, fmt.Errorf("parsing entity extraction JSON %q: %w", parsed.Choices[0].Message.Content, err)
	}
	return result.Entities, false, nil
}
