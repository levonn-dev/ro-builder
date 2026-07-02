# ro-builder Helm chart

Helm chart for the ro-builder API + calc sidecar. Designed to run on
docker-desktop k8s for local dev (via Tilt) and on real clusters in prod.

## Quick start (docker-desktop)

```bash
# One-time cluster setup.
task tilt:bootstrap   # installs Bitnami sealed-secrets controller
task tilt:seal        # encrypts LLM_API_KEY from your .env

# Run.
task tilt:up

# Hit it.
curl http://localhost:8080/healthz
```

## Prod install

```bash
helm upgrade --install ro-builder ./deploy/helm/ro-builder \
  --namespace ro-builder --create-namespace \
  -f deploy/helm/ro-builder/values.prod.yaml
```

You separately apply the prod SealedSecret (sealed against the prod
cluster's controller). The chart references the Secret by name; sealing
happens outside the chart's lifecycle.

## Cluster prerequisites

| Feature                   | Prereq                                              |
|---------------------------|-----------------------------------------------------|
| Sealed Secrets            | Bitnami sealed-secrets controller installed         |
| Ingress                   | Ingress controller (nginx, traefik, contour)        |
| HPA                       | metrics-server installed                            |
| NetworkPolicy enforcement | CNI that enforces (Cilium, Calico, Weave)           |
| ServiceMonitor            | Prometheus Operator CRDs *and* /metrics in the apps |

## Values reference

See `values.yaml` for the full schema and defaults. Key sections:

- `api.*`: image, resources, persistence, probes. `api.replicas` is
  configurable; `api.autoscaling` enables HPA. The default strategy is
  `RollingUpdate` (the API is stateless against Postgres).
- `sidecar.*`: image, replicas, workers, autoscaling, probes. Stateless.
  - `sidecar.calcBackend`: calc backend selection; see
    `calc-sidecar/src/backends/registry.ts`. Use `stub` for chart smoke
    deploys without vendor files. Default `rocalc`.
- `env.*`: non-secret env. Lands in the ConfigMap.
- `secrets.externalSecretName`: name of the Secret the chart references,
  produced by the sealed-secrets controller from a separately-applied
  SealedSecret.
- `ingress.*`, `networkPolicy.*`, `metrics.serviceMonitor.*`: gated,
  default off.

## Database

The API is stateless and connects to Postgres via `DATABASE_URL`, read
from the `ro-builder-env` Secret. `postgresql.enabled` (default `true`)
deploys a bundled in-cluster Postgres StatefulSet using the
`pgvector/pgvector:pg17` image. To use a managed Postgres instead, set
`postgresql.enabled: false` and point `DATABASE_URL` at the external host
in your sealed secret.

## Embeddings (optional)

Semantic retrieval over the saved-build library is optional and off by
default. When disabled, no pgvector extension is required and no pgvector
DDL is run. When enabled, the bootstrap creates the vector column and HNSW
index at startup; pgvector must be installed in the database. The bundled
`pgvector/pgvector:pg17` image already satisfies this requirement for
in-cluster Postgres. For managed Postgres, enable the pgvector extension
through your provider's platform settings before starting the API.

Configure via the `env` ConfigMap block in `values.yaml`:

| Variable | Purpose |
|---|---|
| `EMBEDDING_BASE_URL` | OpenAI-compatible embeddings endpoint; presence enables the feature. |
| `EMBEDDING_MODEL` | Model id (e.g. `text-embedding-nomic-embed-text-v1.5@q8_0`, `nomic-embed-text`, `text-embedding-3-large`). |
| `EMBEDDING_DIM` | Required when `EMBEDDING_BASE_URL` is set (default 768). Must match the model's output dimension; a mismatch causes a startup failure in `Config.Validate`. |
| `EMBEDDING_SEED_MAX_DISTANCE` | Tier A proactive-seed cosine-distance ceiling (default 0.15). |
| `EMBEDDING_SIMILAR_MAX_DISTANCE` | Tier B `get_similar_past_builds` list floor (default 0.5). |
| `EMBEDDING_HNSW_M` / `EMBEDDING_HNSW_EF_CONSTRUCTION` | HNSW index build params (default 16 / 64; reindex to change). |
| `EMBEDDING_HNSW_EF_SEARCH` | HNSW query-time candidate list (default 40; higher = better recall, slower). |

`EMBEDDING_API_KEY` (cloud embedders only) goes in the `ro-builder-env`
Secret alongside `LLM_API_KEY`. See `deploy/k8s/sealed-secrets/`. A pod
reaching a localhost embedder must use the node/host address, not
`localhost` (which resolves to the pod itself).
