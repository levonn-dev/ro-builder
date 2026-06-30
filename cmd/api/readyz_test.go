package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
)

// fakeSidecar boots an httptest server that returns the configured
// status code on GET /healthz. Returns the base URL.
func fakeSidecar(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func openTempLibrary(t *testing.T) *buildlibrary.Library {
	t.Helper()
	if testing.Short() {
		t.Skip("requires Postgres (testcontainers); skipped under -short")
	}
	lib, err := buildlibrary.Open(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("buildlibrary.Open: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	if err := lib.Truncate(context.Background()); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return lib
}

func TestReadyz_AllChecksPass(t *testing.T) {
	url := fakeSidecar(t, http.StatusOK)
	lib := openTempLibrary(t)
	h := readyzHandler(url, &http.Client{Timeout: 2 * time.Second}, lib)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Checks []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("status field: got %q, want ok", body.Status)
	}
	for _, c := range body.Checks {
		if !c.OK {
			t.Errorf("check %q should be ok, got false", c.Name)
		}
	}
}

func TestReadyz_SidecarDown(t *testing.T) {
	url := fakeSidecar(t, http.StatusInternalServerError)
	lib := openTempLibrary(t)
	h := readyzHandler(url, &http.Client{Timeout: 2 * time.Second}, lib)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want %d; body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type: got %q, want application/json", got)
	}
}

func TestReadyz_LibraryNil_SkippedCheck(t *testing.T) {
	url := fakeSidecar(t, http.StatusOK)
	h := readyzHandler(url, &http.Client{Timeout: 2 * time.Second}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Checks []struct {
			Name string `json:"name"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, c := range body.Checks {
		if c.Name == "buildlibrary" {
			t.Errorf("buildlibrary check should be absent when library is nil")
		}
	}
}

func TestReadyz_405ForNonGET(t *testing.T) {
	url := fakeSidecar(t, http.StatusOK)
	h := readyzHandler(url, &http.Client{Timeout: 2 * time.Second}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/readyz", nil)
	h(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Fatalf("Allow header: got %q, want GET", got)
	}
}
