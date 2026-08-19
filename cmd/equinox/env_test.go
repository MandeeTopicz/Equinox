package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnv(t *testing.T) {
	t.Run("missing file is not an error", func(t *testing.T) {
		if err := LoadEnv(filepath.Join(t.TempDir(), "does-not-exist.env")); err != nil {
			t.Fatalf("expected no error for missing file, got: %v", err)
		}
	})

	t.Run("loads variables from file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte("EQUINOX_TEST_ENV_VAR=hello\n"), 0o600); err != nil {
			t.Fatalf("writing temp env file: %v", err)
		}

		if err := LoadEnv(path); err != nil {
			t.Fatalf("LoadEnv: %v", err)
		}

		if got := os.Getenv("EQUINOX_TEST_ENV_VAR"); got != "hello" {
			t.Errorf("EQUINOX_TEST_ENV_VAR = %q, want hello", got)
		}
	})
}
