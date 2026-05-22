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

- `api.*`: image, resources, persistence, probes. Always `replicas: 1`
  with `strategy: Recreate` (SQLite is single-writer).
- `sidecar.*`: image, replicas, workers, autoscaling, probes. Stateless.
- `env.*`: non-secret env. Lands in the ConfigMap.
- `secrets.externalSecretName`: name of the Secret the chart references,
  produced by the sealed-secrets controller from a separately-applied
  SealedSecret.
- `ingress.*`, `networkPolicy.*`, `metrics.serviceMonitor.*`: gated,
  default off.

## Why replicas: 1 on the API?

The buildlibrary is SQLite-backed. SQLite is single-writer and lives in
a process-local file. Multiple API pods would either fight for the PVC
lock (RWO mode) or, if they shared one, corrupt the DB. Horizontal scale
unblocks once the buildlibrary migrates to Postgres.
