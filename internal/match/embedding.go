package match

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Embedder produces one embedding vector per input text, in the same order
// as the input. OpenAIEmbeddingClient is the only implementation used at
// runtime (docs/AI_USAGE.md); it's an interface so Match stays testable
// without calling a real embedding API.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// embedBatchSize caps how many texts go into a single embeddings request,
// well under OpenAI's per-request input limit, so one large `match` run
// doesn't build one oversized request body.
const embedBatchSize = 100

// OpenAIEmbeddingClient calls OpenAI's embeddings API directly over HTTP —
// no SDK dependency, per docs/DECISIONS.md.
type OpenAIEmbeddingClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewOpenAIEmbeddingClient builds a client for the given model (e.g.
// text-embedding-3-small), authenticated with apiKey. A nil httpClient uses
// http.DefaultClient.
func NewOpenAIEmbeddingClient(apiKey, model string, httpClient *http.Client) *OpenAIEmbeddingClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIEmbeddingClient{
		apiKey:     apiKey,
		model:      model,
		baseURL:    "https://api.openai.com",
		httpClient: httpClient,
	}
}

type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// Embed calls POST /v1/embeddings once per embedBatchSize-sized chunk of
// texts and returns one vector per input, in input order.
func (c *OpenAIEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		end := min(start+embedBatchSize, len(texts))
		batch, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		result = append(result, batch...)
	}
	return result, nil
}

func (c *OpenAIEmbeddingClient) embedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	reqBody, err := json.Marshal(openAIEmbeddingRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshaling embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("building embedding request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling OpenAI embeddings API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("OpenAI embeddings API returned %s: %s", resp.Status, string(body))
	}

	var parsed openAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding embedding response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embedding response has %d vectors for %d inputs", len(parsed.Data), len(texts))
	}

	vectors := make([][]float64, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(vectors) {
			return nil, fmt.Errorf("embedding response index %d out of range for %d inputs", d.Index, len(texts))
		}
		vectors[d.Index] = d.Embedding
	}
	return vectors, nil
}
