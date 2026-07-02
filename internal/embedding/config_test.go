package embedding

import "testing"

func TestConfig_EnabledAndValidate(t *testing.T) {
	if (Config{}).Enabled() {
		t.Error("empty config should be disabled")
	}
	c := Config{BaseURL: "http://x/v1", Model: "nomic-embed-text", Dim: 768}
	if !c.Enabled() {
		t.Error("config with BaseURL should be enabled")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("valid config: %v", err)
	}
	if err := (Config{BaseURL: "http://x/v1", Model: "m"}).Validate(); err == nil {
		t.Error("enabled config with Dim=0 must fail Validate")
	}
	if err := (Config{BaseURL: "http://x/v1", Dim: 768}).Validate(); err == nil {
		t.Error("enabled config with empty Model must fail Validate")
	}
	if err := (Config{}).Validate(); err != nil {
		t.Errorf("disabled config is always valid: %v", err)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("EMBEDDING_BASE_URL", "http://localhost:11434/v1")
	t.Setenv("EMBEDDING_MODEL", "nomic-embed-text")
	t.Setenv("EMBEDDING_DIM", "768")
	t.Setenv("EMBEDDING_API_KEY", "sk-x")
	c := LoadConfigFromEnv()
	if c.BaseURL != "http://localhost:11434/v1" || c.Model != "nomic-embed-text" || c.Dim != 768 || c.APIKey != "sk-x" {
		t.Fatalf("unexpected config: %+v", c)
	}
}
