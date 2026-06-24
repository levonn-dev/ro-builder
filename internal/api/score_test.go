package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// fakeSidecar boots an httptest server that captures the request body and
// returns a canned response; lets us test the handler's behavior without
// needing a real Node sidecar in this test file.
func fakeSidecar(t *testing.T, status int, respBody string) (string, *[]byte) {
	t.Helper()
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		captured = b
		w.Header().Set("content-type", "application/json")
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &captured
}

const validDerivedJSON = `{"derived":{"hit":2,"flee":2,"cri":1,"atk":{"base":1,"plus":0},"matk":{"min":1,"max":1},"def":{"hard":0,"soft":1},"mdef":{"hard":0,"soft":1},"aspd":150.3,"maxHp":40,"maxSp":11,"statPointsRemaining":48},"calc_version":"calc-test"}`

// scoreServer mounts a Server with the given scoring URL onto a fresh
// mux. Returns the mux as an http.Handler so tests can drive it via
// httptest.
func scoreServer(t *testing.T, sidecarURL string) http.Handler {
	t.Helper()
	srv := NewServer(scoring.NewClient(sidecarURL, nil), loadCatalog(t))
	mux := http.NewServeMux()
	srv.Mount(mux)
	return mux
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestScoreHandler_HappyPath(t *testing.T) {
	url, captured := fakeSidecar(t, http.StatusOK, validDerivedJSON)
	h := scoreServer(t, url)

	body := `{
		"build": {
			"mode": "pre-renewal",
			"server": "uaro",
			"class": "novice",
			"level": {"base": 1, "job": 1},
			"stats": {"str": 1, "agi": 1, "vit": 1, "int": 1, "dex": 1, "luk": 1}
		}
	}`
	rec := post(t, h, "/score", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := got["derived"]; !ok {
		t.Errorf("response missing derived: %s", rec.Body.String())
	}

	// Verify metadata (mode/server/tier) didn't leak into the sidecar
	// request; those fields are orchestrator-side only.
	var sentToSidecar map[string]any
	if err := json.Unmarshal(*captured, &sentToSidecar); err != nil {
		t.Fatalf("captured sidecar request not JSON: %v / %s", err, *captured)
	}
	if _, ok := sentToSidecar["mode"]; ok {
		t.Errorf("mode leaked into sidecar request: %s", *captured)
	}
	if _, ok := sentToSidecar["server"]; ok {
		t.Errorf("server leaked into sidecar request: %s", *captured)
	}
	if _, ok := sentToSidecar["tier"]; ok {
		t.Errorf("tier leaked into sidecar request: %s", *captured)
	}
}

func TestScoreHandler_ScenarioPropagatesAsEnemy(t *testing.T) {
	url, captured := fakeSidecar(t, http.StatusOK, validDerivedJSON)
	h := scoreServer(t, url)

	body := `{
		"build": {"class": "taekwon", "level": {"base": 1, "job": 1}, "stats": {"str": 1, "agi": 1, "vit": 1, "int": 1, "dex": 1, "luk": 1}},
		"scenario": {"target": 1002}
	}`
	rec := post(t, h, "/score", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}

	var sent map[string]any
	if err := json.Unmarshal(*captured, &sent); err != nil {
		t.Fatalf("captured: %v / %s", err, *captured)
	}
	if sent["enemy"] != float64(1002) {
		t.Errorf("enemy: got %v want 1002; scenario.target should map to enemy", sent["enemy"])
	}
}

func TestScoreHandler_ValidationFailures(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantCode  int
		wantInMsg string
	}{
		{name: "renewal rejected", body: `{"build":{"mode":"renewal","class":"novice","level":{"base":1,"job":1},"stats":{"str":1,"agi":1,"vit":1,"int":1,"dex":1,"luk":1}}}`, wantCode: 400, wantInMsg: "not supported"},
		{name: "unknown mode rejected", body: `{"build":{"mode":"kRO","class":"novice","level":{"base":1,"job":1},"stats":{"str":1,"agi":1,"vit":1,"int":1,"dex":1,"luk":1}}}`, wantCode: 400, wantInMsg: "kRO"},
		{name: "malformed JSON", body: `{not json`, wantCode: 400, wantInMsg: "decode"},
		{name: "refine out of range", body: `{"build":{"class":"novice","level":{"base":1,"job":1},"stats":{"str":1,"agi":1,"vit":1,"int":1,"dex":1,"luk":1},"equipment":{"weapon":{"id":1,"refine":99}}}}`, wantCode: 400, wantInMsg: "refine"},
	}
	// Sidecar should NOT be called for any of these; handler rejects
	// before reaching the client.
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			h := scoreServer(t, srv.URL)

			rec := post(t, h, "/score", tc.body)
			if rec.Code != tc.wantCode {
				t.Errorf("status: got %d want %d; body=%s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantInMsg) {
				t.Errorf("body: got %q want substring %q", rec.Body.String(), tc.wantInMsg)
			}
			if called {
				t.Error("sidecar should not be called when validation fails")
			}
		})
	}
}

func TestScoreHandler_SidecarStatusMapping(t *testing.T) {
	cases := []struct {
		name           string
		sidecarStatus  int
		sidecarBody    string
		wantStatus     int
		wantBodyContns string
	}{
		{name: "sidecar 400 → handler 400", sidecarStatus: 400, sidecarBody: `{"error":"unmapped iRO id 99999"}`, wantStatus: 400, wantBodyContns: "unmapped iRO id"},
		{name: "sidecar 500 → handler 502", sidecarStatus: 500, sidecarBody: `{"error":"shim panicked"}`, wantStatus: 502, wantBodyContns: "shim panicked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, _ := fakeSidecar(t, tc.sidecarStatus, tc.sidecarBody)
			h := scoreServer(t, url)

			rec := post(t, h, "/score", `{"build":{"class":"novice","level":{"base":1,"job":1},"stats":{"str":1,"agi":1,"vit":1,"int":1,"dex":1,"luk":1}}}`)
			if rec.Code != tc.wantStatus {
				t.Errorf("status: got %d want %d", rec.Code, tc.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBodyContns) {
				t.Errorf("body: got %q want substring %q", rec.Body.String(), tc.wantBodyContns)
			}
		})
	}
}
