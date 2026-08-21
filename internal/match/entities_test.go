package match

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeEntityExtractor struct {
	byText map[string][]string
	err    error
}

func (f fakeEntityExtractor) ExtractEntities(ctx context.Context, text string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byText[text], nil
}

func TestEntityGate(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		titleA     string
		titleB     string
		entities   map[string][]string
		wantPassed bool
	}{
		{
			name:   "neither side names an entity",
			titleA: "Will inflation exceed 3%?",
			titleB: "Will inflation stay below 3%?",
			entities: map[string][]string{
				"Will inflation exceed 3%?":     nil,
				"Will inflation stay below 3%?": nil,
			},
			wantPassed: true,
		},
		{
			name:   "same single entity, different phrasing",
			titleA: "Will Marco Rubio win the 2028 US Presidential Election?",
			titleB: "Marco Rubio to win 2028 presidential race?",
			entities: map[string][]string{
				"Will Marco Rubio win the 2028 US Presidential Election?": {"Marco Rubio"},
				"Marco Rubio to win 2028 presidential race?":              {"Marco Rubio"},
			},
			wantPassed: true,
		},
		{
			name:   "real case: single candidate vs. a three-candidate OR-disjunction",
			titleA: "Will Marco Rubio win the 2028 US Presidential Election?",
			titleB: "Will JD Vance, Marco Rubio OR Gavin Newsom win the 2028 presidential election?",
			entities: map[string][]string{
				"Will Marco Rubio win the 2028 US Presidential Election?":                        {"Marco Rubio"},
				"Will JD Vance, Marco Rubio OR Gavin Newsom win the 2028 presidential election?": {"JD Vance", "Marco Rubio", "Gavin Newsom"},
			},
			wantPassed: false,
		},
		{
			name:   "entity case-insensitive match",
			titleA: "Will marco rubio win?",
			titleB: "Will Marco Rubio win?",
			entities: map[string][]string{
				"Will marco rubio win?": {"marco rubio"},
				"Will Marco Rubio win?": {"Marco Rubio"},
			},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := marketAt("polymarket", "", base)
			a.Title = tt.titleA
			b := marketAt("kalshi", "", base)
			b.Title = tt.titleB

			got, err := EntityGate(context.Background(), a, b, fakeEntityExtractor{byText: tt.entities})
			if err != nil {
				t.Fatalf("EntityGate: %v", err)
			}
			if got.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v (reason: %q)", got.Passed, tt.wantPassed, got.Reason)
			}
			if !tt.wantPassed && !strings.Contains(got.Reason, "entity sets differ") {
				t.Errorf("expected reason to mention entity sets differ, got %q", got.Reason)
			}
		})
	}
}

func TestEntityGatePropagatesExtractorError(t *testing.T) {
	base := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	a := marketAt("polymarket", "", base)
	b := marketAt("kalshi", "", base)

	_, err := EntityGate(context.Background(), a, b, fakeEntityExtractor{err: errors.New("api down")})
	if err == nil {
		t.Fatal("expected an error when the extractor fails, got nil")
	}
}

func TestOpenAIEntityExtractor(t *testing.T) {
	var gotBody openAIChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		resp := openAIChatResponse{}
		resp.Choices = []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{{}}
		resp.Choices[0].Message.Content = `{"entities": ["Marco Rubio", "JD Vance"]}`
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	extractor := NewOpenAIEntityExtractor("test-key", "gpt-4o-mini", nil)
	extractor.baseURL = server.URL

	entities, err := extractor.ExtractEntities(context.Background(), "Will JD Vance or Marco Rubio win?")
	if err != nil {
		t.Fatalf("ExtractEntities: %v", err)
	}

	if len(entities) != 2 || entities[0] != "Marco Rubio" || entities[1] != "JD Vance" {
		t.Errorf("unexpected entities: %v", entities)
	}
	if gotBody.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini", gotBody.Model)
	}
	if gotBody.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format.type = %q, want json_object", gotBody.ResponseFormat.Type)
	}
	if len(gotBody.Messages) != 2 || gotBody.Messages[1].Content != "Will JD Vance or Marco Rubio win?" {
		t.Errorf("unexpected messages: %+v", gotBody.Messages)
	}
}

func TestOpenAIEntityExtractorEmptyEntities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIChatResponse{}
		resp.Choices = []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{{}}
		resp.Choices[0].Message.Content = `{"entities": []}`
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	extractor := NewOpenAIEntityExtractor("test-key", "gpt-4o-mini", nil)
	extractor.baseURL = server.URL

	entities, err := extractor.ExtractEntities(context.Background(), "Will inflation exceed 3%?")
	if err != nil {
		t.Fatalf("ExtractEntities: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected no entities, got %v", entities)
	}
}

func TestOpenAIEntityExtractorHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	extractor := NewOpenAIEntityExtractor("bad-key", "gpt-4o-mini", nil)
	extractor.baseURL = server.URL

	if _, err := extractor.ExtractEntities(context.Background(), "x"); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}
