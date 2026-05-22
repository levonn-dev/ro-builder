#!/usr/bin/env bash
# scripts/seal-secret.sh; encrypt LLM_API_KEY from .env into a SealedSecret.
#
# Output: deploy/k8s/sealed-secrets/local.sealed-secret.yaml (gitignored).
# Re-run any time .env changes.

set -euo pipefail

readonly SECRET_NAME="ro-builder-env"
readonly NAMESPACE="ro-builder"
readonly OUTPUT="deploy/k8s/sealed-secrets/local.sealed-secret.yaml"
readonly ENV_FILE=".env"

log() { printf '\033[1;34m[seal]\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m[seal]\033[0m %s\n' "$*" >&2; }

main() {
  if [[ ! -f "$ENV_FILE" ]]; then
    err ".env not found. Run 'task bootstrap' to create it from .env.example."
    exit 1
  fi

  # Source .env in a subshell so we don't pollute the caller's env.
  # shellcheck disable=SC1090
  local LLM_API_KEY
  LLM_API_KEY=$(grep -E '^LLM_API_KEY=' "$ENV_FILE" | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'")

  if [[ -z "$LLM_API_KEY" ]]; then
    err "LLM_API_KEY is empty in $ENV_FILE. Fill it in before sealing."
    exit 1
  fi

  if ! command -v kubeseal >/dev/null 2>&1; then
    err "kubeseal not found on PATH. Run scripts/k8s-bootstrap.sh first."
    exit 1
  fi

  if ! kubectl -n kube-system get deployment sealed-secrets-controller >/dev/null 2>&1; then
    err "sealed-secrets controller not installed. Run scripts/k8s-bootstrap.sh first."
    exit 1
  fi

  log "Building plaintext Secret in-memory..."
  local plaintext
  plaintext=$(kubectl create secret generic "$SECRET_NAME" \
    --namespace "$NAMESPACE" \
    --from-literal=LLM_API_KEY="$LLM_API_KEY" \
    --dry-run=client -o yaml)

  log "Sealing against the cluster's controller key..."
  mkdir -p "$(dirname "$OUTPUT")"
  printf '%s\n' "$plaintext" | kubeseal \
    --controller-namespace kube-system \
    --controller-name sealed-secrets-controller \
    --format yaml \
    > "$OUTPUT"

  log "Wrote $OUTPUT"
  log "Apply with: kubectl apply -f $OUTPUT  (or just run 'task tilt:up')"
}

main "$@"
