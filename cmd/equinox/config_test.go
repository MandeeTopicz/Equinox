package main

import "testing"

func TestLoadConfig(t *testing.T) {
	cfg, err := LoadConfig("../../equinox.yaml")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if len(cfg.Venues) != 3 {
		t.Fatalf("expected 3 venues, got %d", len(cfg.Venues))
	}

	want := map[string]struct {
		baseURL   string
		apiKeyEnv string
	}{
		"polymarket": {"https://gamma-api.polymarket.com", ""},
		"kalshi":     {"https://api.elections.kalshi.com", ""},
		"manifold":   {"https://api.manifold.markets", ""},
	}

	for _, v := range cfg.Venues {
		w, ok := want[v.Name]
		if !ok {
			t.Fatalf("unexpected venue %q", v.Name)
		}
		if v.BaseURL != w.baseURL {
			t.Errorf("venue %s: base_url = %q, want %q", v.Name, v.BaseURL, w.baseURL)
		}
		if v.APIKeyEnv != w.apiKeyEnv {
			t.Errorf("venue %s: api_key_env = %q, want %q", v.Name, v.APIKeyEnv, w.apiKeyEnv)
		}
	}

	if cfg.Match.MinScore != 0.75 {
		t.Errorf("match.min_score = %v, want 0.75", cfg.Match.MinScore)
	}
	if cfg.Match.Embedding.Provider != "openai" {
		t.Errorf("match.embedding.provider = %q, want openai", cfg.Match.Embedding.Provider)
	}
	if cfg.Match.Embedding.Model != "text-embedding-3-small" {
		t.Errorf("match.embedding.model = %q, want text-embedding-3-small", cfg.Match.Embedding.Model)
	}
	if cfg.Match.Embedding.APIKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("match.embedding.api_key_env = %q, want OPENAI_API_KEY", cfg.Match.Embedding.APIKeyEnv)
	}

	if cfg.Database.Path != "./equinox.db" {
		t.Errorf("database.path = %q, want ./equinox.db", cfg.Database.Path)
	}
}

func TestVenueConfigAPIKey(t *testing.T) {
	t.Run("no api_key_env returns empty string, no error", func(t *testing.T) {
		v := VenueConfig{Name: "manifold"}
		key, err := v.APIKey()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "" {
			t.Errorf("key = %q, want empty", key)
		}
	})

	t.Run("missing env var errors", func(t *testing.T) {
		v := VenueConfig{Name: "kalshi", APIKeyEnv: "EQUINOX_TEST_UNSET_VAR"}
		t.Setenv("EQUINOX_TEST_UNSET_VAR", "")
		if _, err := v.APIKey(); err == nil {
			t.Fatal("expected error for unset env var, got nil")
		}
	})

	t.Run("set env var resolves", func(t *testing.T) {
		v := VenueConfig{Name: "kalshi", APIKeyEnv: "EQUINOX_TEST_SET_VAR"}
		t.Setenv("EQUINOX_TEST_SET_VAR", "secret-value")
		key, err := v.APIKey()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "secret-value" {
			t.Errorf("key = %q, want secret-value", key)
		}
	})
}
