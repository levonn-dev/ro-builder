#!/usr/bin/env bash
# scripts/k8s-bootstrap.sh; one-shot k8s prerequisites for local dev.
#
# Verifies required CLIs are on PATH, installs the Bitnami sealed-secrets
# controller into kube-system, and creates the ro-builder namespace.
# Idempotent; safe to re-run.

set -euo pipefail

# Keep in sync with SEALED_SECRETS_VERSION in k8s-teardown.sh.
readonly SEALED_SECRETS_VERSION="v0.27.0"
readonly SEALED_SECRETS_URL="https://github.com/bitnami-labs/sealed-secrets/releases/download/${SEALED_SECRETS_VERSION}/controller.yaml"
readonly NAMESPACE="ro-builder"

log() { printf '\033[1;34m[bootstrap]\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m[bootstrap]\033[0m %s\n' "$*" >&2; }

check_cli() {
  local name="$1" hint="$2"
  if ! command -v "$name" >/dev/null 2>&1; then
    err "Required CLI '$name' not found on PATH. $hint"
    exit 1
  fi
}

main() {
  log "Checking required CLIs..."
  check_cli kubectl   "Install Docker Desktop (which bundles kubectl) or follow https://kubernetes.io/docs/tasks/tools/."
  check_cli helm      "https://helm.sh/docs/intro/install/"
  check_cli kubeseal  "https://github.com/bitnami-labs/sealed-secrets#kubeseal"
  check_cli tilt      "https://docs.tilt.dev/install.html"

  log "Verifying kubectl can reach the cluster..."
  if ! kubectl cluster-info >/dev/null 2>&1; then
    err "kubectl cannot reach the cluster. Is docker-desktop k8s enabled?"
    exit 1
  fi

  local context
  context=$(kubectl config current-context)
  if [[ "$context" != "docker-desktop" ]]; then
    err "Current kubectl context is '$context', not 'docker-desktop'."
    err "This script is meant for the local docker-desktop cluster."
    err "Run 'kubectl config use-context docker-desktop' first."
    exit 1
  fi
  log "Context OK: $context"

  log "Installing sealed-secrets controller ($SEALED_SECRETS_VERSION)..."
  kubectl apply -f "$SEALED_SECRETS_URL"

  log "Waiting for sealed-secrets controller to become available..."
  kubectl -n kube-system wait --for=condition=available --timeout=120s \
    deployment/sealed-secrets-controller

  log "Creating namespace '$NAMESPACE' (if missing)..."
  kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

  # Attempt seal immediately if LLM_API_KEY is already set in .env.
  # Users who export the key before running bootstrap get a one-shot setup.
  # Users who haven't filled it in yet get the explicit next-steps message.
  local llm_key=""
  if [[ -f .env ]]; then
    llm_key=$(grep -E '^LLM_API_KEY=' .env | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'")
  fi

  if [[ -n "$llm_key" ]]; then
    log "LLM_API_KEY found; sealing now..."
    "$(dirname "$0")/seal-secret.sh"
    cat <<EOF

[bootstrap] Done. Run: task tilt:up

EOF
  else
    cat <<EOF

[bootstrap] Done.

Next steps:
  1. Fill in LLM_API_KEY in .env
  2. task tilt:seal      ; encrypt it into a SealedSecret
  3. task tilt:up        ; boot the full stack via Tilt

EOF
  fi
}

main "$@"
