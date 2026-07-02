# Sealed Secrets

Encrypted secrets for ro-builder. Decrypted in-cluster by the
[Bitnami Sealed Secrets controller](https://github.com/bitnami-labs/sealed-secrets).

## Files

- `local.sealed-secret.yaml.example`: committed placeholder for the
  local (docker-desktop) cluster's sealed secret.
- `local.sealed-secret.yaml`: gitignored. Produced by `task tilt:seal`,
  contains the real encrypted `LLM_API_KEY`, `POSTGRES_PASSWORD`,
  `DATABASE_URL`, and optionally `EMBEDDING_API_KEY` from your `.env`.
- `prod.sealed-secret.yaml.example`: committed template for a prod
  cluster. Sealing for prod is operator-only and happens out of band.

## Secret keys

| Key | Required | Purpose |
|---|---|---|
| `LLM_API_KEY` | For `/generate` | LLM provider credential |
| `POSTGRES_PASSWORD` | Yes | Database password |
| `DATABASE_URL` | Yes | Full connection string |
| `EMBEDDING_API_KEY` | Optional | Cloud embedding provider (omit for a local embedder) |

## How sealing works (local)

1. `task tilt:bootstrap` installs the sealed-secrets controller in
   `kube-system`. The controller generates a cluster-bound RSA key pair
   on first start.
2. `task tilt:seal` reads your local `.env`, builds an in-memory Secret
   manifest with `LLM_API_KEY`, `POSTGRES_PASSWORD`, `DATABASE_URL`, and
   optionally `EMBEDDING_API_KEY` (cloud embedders only; omit for a local
   embedder), pipes it through `kubeseal` (which fetches the controller's
   public key), and writes the encrypted `SealedSecret` to
   `local.sealed-secret.yaml`.
3. Tilt applies `local.sealed-secret.yaml` alongside the Helm chart.
   The controller decrypts it into a normal `Secret` named
   `ro-builder-env` that the api Deployment mounts via `envFrom`.

## Cluster reset gotcha

If you reset docker-desktop's k8s, the sealed-secrets controller
generates **new** keys. The committed `local.sealed-secret.yaml` becomes
undecryptable. Run `task tilt:seal` to re-seal.

## Rotation

Edit `.env`, run `task tilt:seal`. Tilt detects the file change,
reapplies, and the controller decrypts the new value. The api pod
restarts to pick it up: the deployment carries both a `checksum/config`
annotation (hashing the rendered ConfigMap) and a `checksum/secret`
annotation (hashing the live Secret's `.data` via Helm's `lookup`
function). On the next `helm upgrade`/Tilt sync after a re-seal, the
hash changes and the deployment rolls automatically; no manual
`kubectl rollout restart` required.
