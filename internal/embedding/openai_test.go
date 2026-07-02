package embedding

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeEmbeddings(t *testing.T, status int, body string) (string, *map[string]any, *http.Header) {
	t.Helper()
	var captured map[string]any
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &captured, &hdr
}

func TestClient_Embed_OK(t *testing.T) {
	url, captured, hdr := fakeEmbeddings(t, 200, `{"data":[{"embedding":[0.1,0.2,0.3],"index":0},{"embedding":[0.4,0.5,0.6],"index":1}],"model":"m"}`)
	c := New(Config{BaseURL: url, Model: "m", Dim: 3, APIKey: "sk-x"})
	vecs, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 3 || vecs[1][2] != 0.6 {
		t.Fatalf("bad vectors: %v", vecs)
	}
	if hdr.Get("authorization") != "Bearer sk-x" {
		t.Errorf("authorization: %q", hdr.Get("authorization"))
	}
	if (*captured)["model"] != "m" {
		t.Errorf("model not sent: %v", *captured)
	}
	if (*captured)["dimensions"].(float64) != 3 {
		t.Errorf("dimensions not sent: %v", *captured)
	}
	if c.Dimensions() != 3 || c.ModelID() != "m@3" {
		t.Errorf("Dimensions/ModelID: %d %q", c.Dimensions(), c.ModelID())
	}
}

func TestClient_Embed_DimMismatch(t *testing.T) {
	url, _, _ := fakeEmbeddings(t, 200, `{"data":[{"embedding":[0.1,0.2],"index":0}]}`)
	c := New(Config{BaseURL: url, Model: "m", Dim: 3})
	if _, err := c.Embed(context.Background(), []string{"a"}); err == nil {
		t.Fatal("expected dimension-mismatch error")
	}
}

func TestClient_Embed_HTTPError(t *testing.T) {
	url, _, _ := fakeEmbeddings(t, 500, `{"error":{"message":"boom"}}`)
	c := New(Config{BaseURL: url, Model: "m", Dim: 3})
	if _, err := c.Embed(context.Background(), []string{"a"}); err == nil {
		t.Fatal("expected HTTP error")
	}
}
