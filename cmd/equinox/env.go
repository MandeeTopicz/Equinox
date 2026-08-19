package main

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

// LoadEnv loads environment variables from a .env file at path. A missing
// file is not an error — real deployments may set env vars another way.
func LoadEnv(path string) error {
	err := godotenv.Load(path)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
