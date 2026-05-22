package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
)

// readyzCheckTimeout caps each subordinate probe so an unresponsive
// sidecar or wedged DB connection can't stall the kubelet's probe loop.
const readyzCheckTimeout = 3 * time.Second

// readyzHandler reports whether the API can service requests right now.
// Distinct from /healthz (liveness); /readyz returns 503 when a
// downstream the API depends on is unreachable, so the kubelet can pull
// the pod out of the Service while leaving the container running. Pings
// the sidecar HTTP root and the buildlibrary DB; library is nil-skipped
// when the API runs without one (tests, --no-library mode if ever added).
func readyzHandler(sidecarURL string, httpC *http.Client, lib *buildlibrary.Library) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		type checkResult struct {
			Name  string `json:"name"`
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}
		var results []checkResult
		allOK := true

		// Sidecar: GET /healthz (matches the sidecar's existing endpoint).
		sidecarOK, sidecarErr := pingSidecar(r.Context(), httpC, sidecarURL)
		results = append(results, checkResult{Name: "sidecar", OK: sidecarOK, Error: errString(sidecarErr)})
		if !sidecarOK {
			allOK = false
		}

		// Library: db.PingContext. Skipped when nil so test wirings or
		// minimal deployments don't fail readyz just for lack of a DB.
		if lib != nil {
			ctx, cancel := context.WithTimeout(r.Context(), readyzCheckTimeout)
			defer cancel()
			libErr := lib.Ping(ctx)
			results = append(results, checkResult{Name: "buildlibrary", OK: libErr == nil, Error: errString(libErr)})
			if libErr != nil {
				allOK = false
			}
		}

		w.Header().Set("Content-Type", "application/json")
		status := http.StatusOK
		if !allOK {
			status = http.StatusServiceUnavailable
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": statusString(allOK),
			"checks": results,
		})
	}
}

// pingSidecar fires a short GET against the sidecar's /healthz. Any
// non-2xx response or transport error is a readiness fail.
func pingSidecar(parent context.Context, httpC *http.Client, baseURL string) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, readyzCheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return false, err
	}
	resp, err := httpC.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("sidecar returned %d", resp.StatusCode)
	}
	return true, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func statusString(ok bool) string {
	if ok {
		return "ok"
	}
	return "degraded"
}
