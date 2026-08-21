package match

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
}

// NewOpenAIEntityExtractor builds an extractor for the given chat model
// (e.g. gpt-4o-mini), authenticated with apiKey. A nil httpClient uses
// http.DefaultClient.
func NewOpenAIEntityExtractor(apiKey, model string, httpClient *http.Client) *OpenAIEntityExtractor {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIEntityExtractor{
		apiKey:     apiKey,
		model:      model,
		baseURL:    "https://api.openai.com",
		httpClient: httpClient,
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

func (c *OpenAIEntityExtractor) ExtractEntities(ctx context.Context, text string) ([]string, error) {
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
		return nil, fmt.Errorf("marshaling entity extraction request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("building entity extraction request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling OpenAI chat completions API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("OpenAI chat completions API returned %s: %s", resp.Status, string(body))
	}

	var parsed openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding chat completion response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("chat completion response had no choices")
	}

	var result entityExtractionResult
	if err := json.Unmarshal([]byte(parsed.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("parsing entity extraction JSON %q: %w", parsed.Choices[0].Message.Content, err)
	}
	return result.Entities, nil
}
