#!/usr/bin/env bash
# scripts/k8s-teardown.sh; inverse of k8s-bootstrap.sh for the platform side.
#
# Removes the Bitnami sealed-secrets controller from kube-system: the
# manifest objects plus the runtime-generated sealing-key secrets the
# controller creates on first start. Deleting the manifest also deletes the
# SealedSecret CRD, which cascades to any SealedSecret CRs still in the
# cluster; run 'task tilt:nuke' first to take the app namespace down
# cleanly. Afterwards the gitignored local.sealed-secret.yaml is stale
# (encrypted to the deleted keys); the next k8s-bootstrap.sh re-seals from
# .env, or run 'task tilt:seal' manually.
#
# Idempotent; safe to re-run.

set -euo pipefail

# Keep in sync with SEALED_SECRETS_VERSION in k8s-bootstrap.sh.
readonly SEALED_SECRETS_VERSION="v0.27.0"
readonly SEALED_SECRETS_URL="https://github.com/bitnami-labs/sealed-secrets/releases/download/${SEALED_SECRETS_VERSION}/controller.yaml"

log() { printf '\033[1;34m[teardown]\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m[teardown]\033[0m %s\n' "$*" >&2; }

main() {
  if ! command -v kubectl >/dev/null 2>&1; then
    err "Required CLI 'kubectl' not found on PATH."
    exit 1
  fi

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

  log "Removing sealed-secrets controller ($SEALED_SECRETS_VERSION)..."
  kubectl delete -f "$SEALED_SECRETS_URL" --ignore-not-found

  log "Removing generated sealing-key secrets from kube-system..."
  kubectl -n kube-system delete secret -l sealedsecrets.bitnami.com/sealed-secrets-key

  cat <<EOF

[teardown] Done. Controller and sealing keys removed.
[teardown] local.sealed-secret.yaml (if present) is now stale; the next
[teardown] 'task tilt:bootstrap' reinstalls the controller and re-seals
[teardown] from .env (or run 'task tilt:seal' manually).

EOF
}

main "$@"
