#!/usr/bin/env bash
# scripts/k8s-e2e.sh; end-to-end smoke against the Helm chart on the
# current k8s cluster (intended: docker-desktop). Parallel to
# scripts/docker-e2e.sh (compose path).
#
# What it does:
#   1. helm install ro-builder-e2e in a temp namespace.
#   2. Create a plain Secret (LLM_API_KEY from env or 'stub'); the
#      committed SealedSecret is namespace-bound to ro-builder and can't
#      decrypt into the temp namespace.
#   3. Wait for both deployments to roll out (ready replicas).
#   4. kubectl port-forward and hit /healthz, then POST /score.
#   5. If LLM_API_KEY is a real value (not 'stub'), POST /generate + poll
#      until success.
#   6. helm uninstall + delete namespace.
#
# Exit code: 0 success, non-zero on any failure.

set -euo pipefail

readonly RELEASE="ro-builder-e2e"
readonly NAMESPACE="ro-builder-e2e"
readonly LOCAL_PORT="18080"
readonly TIMEOUT="180s"

PF_PID=""

log() { printf '\033[1;34m[k8s-e2e]\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m[k8s-e2e]\033[0m %s\n' "$*" >&2; }

cleanup() {
  local exit_code=$?
  if [[ -n "$PF_PID" ]]; then
    log "Stopping port-forward (pid $PF_PID)..."
    kill "$PF_PID" 2>/dev/null || true
    wait "$PF_PID" 2>/dev/null || true
  fi
  log "Uninstalling release + deleting namespace..."
  helm uninstall "$RELEASE" -n "$NAMESPACE" >/dev/null 2>&1 || true
  kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  exit $exit_code
}
trap cleanup EXIT INT TERM

main() {
  log "Checking prereqs..."
  command -v helm >/dev/null || { err "helm not on PATH"; exit 1; }
  command -v kubectl >/dev/null || { err "kubectl not on PATH"; exit 1; }
  kubectl cluster-info >/dev/null 2>&1 || { err "kubectl cannot reach cluster"; exit 1; }

  log "Creating namespace $NAMESPACE..."
  kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

  # The chart references a Secret by name. SealedSecret's default scope is
  # name+namespace-bound, so the committed local.sealed-secret.yaml (which
  # targets the 'ro-builder' namespace) won't decrypt into 'ro-builder-e2e'.
  # Always create a plain Secret here using whatever LLM_API_KEY is in the
  # shell env (Taskfile.yml's dotenv loads .env automatically when invoked
  # via `task tilt:e2e`; when run directly, source .env first or export the
  # var). If LLM_API_KEY is unset, we use 'stub' and skip the /generate slice.
  # DB creds use fixed local defaults (POSTGRES_PASSWORD=robuilder) matching
  # the bundled Postgres StatefulSet; DATABASE_URL uses the release-scoped
  # service name.
  if [[ -z "${LLM_API_KEY:-}" && -f .env ]]; then
    # shellcheck disable=SC1091
    LLM_API_KEY=$(grep -E '^LLM_API_KEY=' .env | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'")
    export LLM_API_KEY
  fi
  log "Applying namespaced Secret (LLM_API_KEY: ${LLM_API_KEY:+set}${LLM_API_KEY:-stub}; POSTGRES_PASSWORD + DATABASE_URL: bundled defaults)..."
  kubectl -n "$NAMESPACE" create secret generic ro-builder-env \
    --from-literal=LLM_API_KEY="${LLM_API_KEY:-stub}" \
    --from-literal=POSTGRES_PASSWORD=robuilder \
    --from-literal=DATABASE_URL="postgres://robuilder:robuilder@${RELEASE}-postgres:5432/robuilder?sslmode=disable" \
    --dry-run=client -o yaml | kubectl apply -f -

  log "Installing chart ($RELEASE in $NAMESPACE)..."
  helm upgrade --install "$RELEASE" deploy/helm/ro-builder \
    --namespace "$NAMESPACE" \
    -f deploy/helm/ro-builder/values.local.yaml \
    --set api.image.pullPolicy=IfNotPresent \
    --set sidecar.image.pullPolicy=IfNotPresent \
    --wait --timeout "$TIMEOUT"

  log "Waiting for deployments to be Available..."
  kubectl -n "$NAMESPACE" wait --for=condition=available --timeout="$TIMEOUT" \
    deployment/"$RELEASE"-api deployment/"$RELEASE"-sidecar

  log "Starting port-forward: localhost:$LOCAL_PORT -> svc/$RELEASE-api:8080"
  kubectl -n "$NAMESPACE" port-forward svc/"$RELEASE"-api "$LOCAL_PORT":8080 >/dev/null 2>&1 &
  PF_PID=$!
  sleep 2

  log "GET /healthz..."
  if ! curl -fsS "http://localhost:$LOCAL_PORT/healthz" >/dev/null; then
    err "/healthz failed"
    exit 1
  fi
  log "/healthz OK"

  log "POST /score (empty body; Novice default)..."
  local score_resp
  score_resp=$(curl -fsS -X POST "http://localhost:$LOCAL_PORT/score" \
    -H 'content-type: application/json' -d '{}')
  echo "$score_resp" | head -c 200
  echo
  if ! echo "$score_resp" | grep -q '"derived"'; then
    err "/score response missing 'derived' field"
    exit 1
  fi
  log "/score OK"

  # /generate slice is conditional on a real LLM_API_KEY being available.
  if [[ -n "${LLM_API_KEY:-}" && "${LLM_API_KEY}" != "stub" ]]; then
    log "POST /generate (smoke; quick TK scenario)..."
    # Minimal valid request. Server is in pre-renewal mode by default.
    local gen_id
    gen_id=$(curl -fsS -X POST "http://localhost:$LOCAL_PORT/generate" \
      -H 'content-type: application/json' \
      -d '{"class":"taekwon_kid","server":"uaro","playstyle":"pvm","description":"k8s e2e smoke"}' \
      | grep -oE '"id":"[^"]+"' | head -n1 | cut -d'"' -f4)
    if [[ -z "$gen_id" ]]; then
      err "/generate did not return an id"
      exit 1
    fi
    log "Enqueued generation $gen_id; polling..."
    local status=""
    for _ in $(seq 1 60); do
      sleep 5
      status=$(curl -fsS "http://localhost:$LOCAL_PORT/generations/$gen_id" | grep -oE '"status":"[^"]+"' | head -n1 | cut -d'"' -f4)
      log "  status=$status"
      if [[ "$status" == "completed" ]]; then
        log "/generate OK"
        break
      fi
      if [[ "$status" == "failed" ]]; then
        err "/generate failed"
        exit 1
      fi
    done
    if [[ "$status" != "completed" ]]; then
      err "/generate did not complete within timeout (last status: ${status:-none})"
      exit 1
    fi
  else
    log "LLM_API_KEY unset/stub; skipping /generate slice."
  fi

  log "All checks passed."
}

main "$@"
