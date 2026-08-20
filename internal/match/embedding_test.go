package match

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbeddingClientEmbed(t *testing.T) {
	var gotRequests []openAIEmbeddingRequest
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")

		var req openAIEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		gotRequests = append(gotRequests, req)

		var resp openAIEmbeddingResponse
		for i, text := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: []float64{float64(len(text)), 0}, Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIEmbeddingClient("test-key", "text-embedding-3-small", nil)
	client.baseURL = server.URL

	texts := []string{"hello", "a longer piece of text"}
	vectors, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-key")
	}
	if len(gotRequests) != 1 {
		t.Fatalf("expected 1 request for a small batch, got %d", len(gotRequests))
	}
	if gotRequests[0].Model != "text-embedding-3-small" {
		t.Errorf("model = %q, want text-embedding-3-small", gotRequests[0].Model)
	}

	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	if vectors[0][0] != float64(len("hello")) {
		t.Errorf("vector 0 out of order or wrong: %v", vectors[0])
	}
	if vectors[1][0] != float64(len("a longer piece of text")) {
		t.Errorf("vector 1 out of order or wrong: %v", vectors[1])
	}
}

func TestOpenAIEmbeddingClientBatchesLargeInput(t *testing.T) {
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openAIEmbeddingRequest
		json.NewDecoder(r.Body).Decode(&req)
		batchSizes = append(batchSizes, len(req.Input))

		var resp openAIEmbeddingResponse
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: []float64{1}, Index: i})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIEmbeddingClient("k", "m", nil)
	client.baseURL = server.URL

	texts := make([]string, embedBatchSize+10)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}

	vectors, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != len(texts) {
		t.Fatalf("expected %d vectors, got %d", len(texts), len(vectors))
	}
	if len(batchSizes) != 2 || batchSizes[0] != embedBatchSize || batchSizes[1] != 10 {
		t.Errorf("expected batches [%d, 10], got %v", embedBatchSize, batchSizes)
	}
}

func TestOpenAIEmbeddingClientHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	client := NewOpenAIEmbeddingClient("bad-key", "m", nil)
	client.baseURL = server.URL

	if _, err := client.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}
