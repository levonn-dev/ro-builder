# Tilt overlay

`helm-values.yaml` is layered on top of `deploy/helm/ro-builder/values.local.yaml`
by the Tiltfile. It carries Tilt-only choices (image refs Tilt manages,
`pullPolicy: Never` so docker-desktop k8s reads locally-built images).

You generally don't edit this; Tilt does, via its image graph + helm
extension wiring in the root `Tiltfile`.

The sidecar's calc backend is selected via the chart's `sidecar.calcBackend` value (default `rocalc`). For local
work that doesn't need real calc numbers, override it via `deploy/helm/ro-builder/values.local.yaml`.
