package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config mirrors the shape of equinox.yaml (see README.md's Configuration section).
type Config struct {
	Venues   []VenueConfig  `yaml:"venues"`
	Match    MatchConfig    `yaml:"match"`
	Database DatabaseConfig `yaml:"database"`
}

type VenueConfig struct {
	Name      string `yaml:"name"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type MatchConfig struct {
	MinScore  float64         `yaml:"min_score"`
	Embedding EmbeddingConfig `yaml:"embedding"`
}

type EmbeddingConfig struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// LoadConfig reads and parses the equinox.yaml config file at path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	return &cfg, nil
}

// APIKey resolves a venue's API key from the environment variable named in
// api_key_env. Venues with no api_key_env (e.g. Manifold) return an empty
// string with no error.
func (v VenueConfig) APIKey() (string, error) {
	if v.APIKeyEnv == "" {
		return "", nil
	}
	key := os.Getenv(v.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("environment variable %s is not set (required for venue %s)", v.APIKeyEnv, v.Name)
	}
	return key, nil
}

// APIKey resolves the embedding provider's API key from the environment
// variable named in api_key_env.
func (e EmbeddingConfig) APIKey() (string, error) {
	if e.APIKeyEnv == "" {
		return "", fmt.Errorf("embedding config has no api_key_env set")
	}
	key := os.Getenv(e.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("environment variable %s is not set (required for embedding provider)", e.APIKeyEnv)
	}
	return key, nil
}
