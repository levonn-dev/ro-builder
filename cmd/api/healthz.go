package main

import (
	"net/http"
)

// healthzHandler is a liveness probe target. Returns 200 with a minimal JSON
// body. Intentionally does not touch SQLite or the sidecar; those failure
// modes belong to richer probes (or to /readyz, when we add one).
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
