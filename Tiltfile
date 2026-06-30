# Tiltfile: local dev orchestration for ro-builder on docker-desktop k8s.
#
# Run: `task tilt:up` (or `tilt up` directly).
#
# Layout:
#   1. Preflight: verify sealed-secrets controller + local SealedSecret exist.
#   2. SealedSecret apply: chart references the Secret by name; this provides it.
#   3. Builds: local Go binary build + dockerfile builds with live_update.
#   4. Helm: template the chart with values.local.yaml + tilt overlay.
#   5. Resources: k8s_resource() bindings (port_forwards, deps).

load('ext://restart_process', 'RESTART_FILE')

# ----- preflight -----

local_resource(
    'preflight-sealed-secrets-controller',
    cmd='kubectl -n kube-system get deployment sealed-secrets-controller >/dev/null 2>&1 || (echo "sealed-secrets controller not installed. Run: task tilt:bootstrap"; exit 1)',
    labels=['preflight'],
)

local_resource(
    'preflight-sealed-secret-file',
    cmd='test -f deploy/k8s/sealed-secrets/local.sealed-secret.yaml || (echo "local sealed secret missing. Run: task tilt:seal"; exit 1)',
    labels=['preflight'],
)

# ----- sealed-secret apply -----
# Apply the local SealedSecret. The chart references the Secret by name;
# this resource creates the SealedSecret which the controller decrypts.
# Guard against the file not yet existing (run `task tilt:seal` first).
# Without this the Tiltfile crashes at parse time; with the guard, Tilt
# starts up and the preflight resource surfaces the actionable error in
# the UI.

_sealed_secret_file = 'deploy/k8s/sealed-secrets/local.sealed-secret.yaml'
_sealed_secret_ready = os.path.exists(_sealed_secret_file)

if _sealed_secret_ready:
    k8s_yaml(_sealed_secret_file)
    k8s_resource(
        new_name='sealed-secret',
        objects=['ro-builder-env:SealedSecret'],
        labels=['secrets'],
        resource_deps=['preflight-sealed-secrets-controller', 'preflight-sealed-secret-file'],
    )

# ----- builds -----

# Go API: build outside the container, sync the binary in, restart process.
# Faster than rebuilding the image on every Go change.
local_resource(
    'api-binary',
    cmd='mkdir -p tmp && CGO_ENABLED=0 GOOS=linux go build -o tmp/api ./cmd/api',
    deps=['cmd/api', 'internal', 'configs', 'go.mod', 'go.sum'],
    labels=['build'],
)

docker_build(
    'ro-builder-api',
    '.',
    dockerfile='docker/api.Dockerfile',
    only=['cmd', 'internal', 'configs', 'go.mod', 'go.sum', 'api'],
    live_update=[
        sync('./tmp/api', '/usr/local/bin/api'),
        run('touch ' + RESTART_FILE),
    ],
)

# Sidecar: sync TS source + server.ts, restart node (node 24 runs TS directly
# via strip mode, no build step).
docker_build(
    'ro-builder-sidecar',
    '.',
    dockerfile='docker/sidecar.Dockerfile',
    only=['calc-sidecar', 'docker/sidecar.Dockerfile'],
    ignore=['calc-sidecar/node_modules', 'calc-sidecar/coverage'],
    live_update=[
        sync('./calc-sidecar/src', '/app/src'),
        sync('./calc-sidecar/server.ts', '/app/server.ts'),
        run('touch ' + RESTART_FILE),
    ],
)

# ----- helm -----
# Preflight the values files we hand to helm(). Without these checks the
# helm() call fails with a less-helpful "open ...: no such file" at parse
# time and Tilt's UI never starts. Mirrors the sealed-secret preflight
# pattern above; fail fast with an actionable message.

for _values_file in [
    'deploy/helm/ro-builder/values.local.yaml',
    'deploy/tilt/helm-values.yaml',
]:
    if not os.path.exists(_values_file):
        fail("required helm values file missing: " + _values_file)

yaml = helm(
    'deploy/helm/ro-builder',
    name='ro-builder',
    namespace='ro-builder',
    values=[
        'deploy/helm/ro-builder/values.local.yaml',
        'deploy/tilt/helm-values.yaml',
    ],
)
k8s_yaml(yaml)

# ----- resources -----

_secret_dep = ['sealed-secret'] if _sealed_secret_ready else []

k8s_resource(
    'ro-builder-postgres',
    labels=['db'],
)

k8s_resource(
    'ro-builder-api',
    port_forwards=['8080:8080'],
    resource_deps=_secret_dep + ['api-binary', 'ro-builder-postgres'],
    labels=['app'],
)

k8s_resource(
    'ro-builder-sidecar',
    resource_deps=_secret_dep,
    labels=['app'],
)
