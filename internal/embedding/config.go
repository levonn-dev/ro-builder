package embedding

import (
	"fmt"
	"os"
	"strconv"
)

// Config is populated from EMBEDDING_* env vars by LoadConfigFromEnv.
// BaseURL presence is the on/off switch; when set, Model and Dim are
// required (Dim is both the request "dimensions" and the storage column N).
type Config struct {
	BaseURL string
	Model   string
	APIKey  string
	Dim     int
}

// Enabled reports whether embeddings are configured at all.
func (c Config) Enabled() bool { return c.BaseURL != "" }

// Validate returns an error if the config is enabled but incomplete. A
// disabled config (no BaseURL) is always valid.
func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	if c.Model == "" {
		return fmt.Errorf("embedding: EMBEDDING_MODEL is required when EMBEDDING_BASE_URL is set")
	}
	if c.Dim <= 0 {
		return fmt.Errorf("embedding: EMBEDDING_DIM (>0) is required when EMBEDDING_BASE_URL is set")
	}
	return nil
}

// LoadConfigFromEnv reads EMBEDDING_* into a Config.
//
//	EMBEDDING_BASE_URL  OpenAI-compatible /v1 root; presence enables the feature
//	EMBEDDING_MODEL     model id (e.g. nomic-embed-text, text-embedding-3-large)
//	EMBEDDING_API_KEY   optional bearer (cloud); ignored by local servers
//	EMBEDDING_DIM       vector dimension / tier; required when enabled
func LoadConfigFromEnv() Config {
	dim := 0
	if v := os.Getenv("EMBEDDING_DIM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dim = n
		}
	}
	return Config{
		BaseURL: os.Getenv("EMBEDDING_BASE_URL"),
		Model:   os.Getenv("EMBEDDING_MODEL"),
		APIKey:  os.Getenv("EMBEDDING_API_KEY"),
		Dim:     dim,
	}
}
