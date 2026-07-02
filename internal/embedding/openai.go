package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultHTTPTimeout = 60 * time.Second

// Client is an OpenAI-compatible /v1/embeddings provider. Construct with New.
type Client struct {
	baseURL string
	model   string
	apiKey  string
	dim     int
	httpC   *http.Client
}

type Option func(*Client)

// WithHTTPClient overrides the default timeout-bounded client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpC = h } }

// New constructs the client. cfg.Dim is authoritative for Dimensions().
func New(cfg Config, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		dim:     cfg.Dim,
		httpC:   &http.Client{Timeout: defaultHTTPTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) Dimensions() int { return c.dim }
func (c *Client) ModelID() string { return c.model + "@" + strconv.Itoa(c.dim) }

type embedRequest struct {
	Input      []string `json:"input"`
	Model      string   `json:"model"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

type embedErrEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Embed posts texts to {baseURL}/embeddings and returns one vector per
// input, ordered by the response index. Each vector is validated to have
// length Dimensions(); a mismatch is a configuration error surfaced loudly.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Input: texts, Model: c.model, Dimensions: c.dim})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embed request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpC.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call embeddings: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embeddings response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var env embedErrEnvelope
		if json.Unmarshal(raw, &env) == nil && env.Error.Message != "" {
			return nil, fmt.Errorf("embeddings HTTP %d (%s): %s", resp.StatusCode, env.Error.Type, env.Error.Message)
		}
		return nil, fmt.Errorf("embeddings HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out embedResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: got %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(out.Data))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) {
			return nil, fmt.Errorf("embeddings: out-of-range index %d", d.Index)
		}
		if len(d.Embedding) != c.dim {
			return nil, fmt.Errorf("embeddings: model returned %d dims, configured EMBEDDING_DIM=%d (pick a matching model/tier)", len(d.Embedding), c.dim)
		}
		vecs[d.Index] = d.Embedding
	}
	return vecs, nil
}
