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
deploys a bundled in-cluster Postgres StatefulSet. To use a managed
Postgres instead, set `postgresql.enabled: false` and point `DATABASE_URL`
at the external host in your sealed secret.
