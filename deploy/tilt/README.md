# Tilt overlay

`helm-values.yaml` is layered on top of `deploy/helm/ro-builder/values.local.yaml`
by the Tiltfile. It carries Tilt-only choices (image refs Tilt manages,
`pullPolicy: Never` so docker-desktop k8s reads locally-built images).

You generally don't edit this; Tilt does, via its image graph + helm
extension wiring in the root `Tiltfile`.

The sidecar's calc backend is selected via the chart's `sidecar.calcBackend` value (default `rocalc`). For local
work that doesn't need real calc numbers, override it via `deploy/helm/ro-builder/values.local.yaml`.

## Embeddings (optional)

Embedding-based RAG is off by default in the Tilt dev environment. To exercise the full embedding pipeline locally,
run an OpenAI-compatible embeddings endpoint (LM Studio, Ollama, llama.cpp, ...) and set the following in your `.env`
before `task tilt:up`:

```
# LM Studio (default): load an embedding model in the Developer tab, then
EMBEDDING_BASE_URL=http://localhost:1234/v1
EMBEDDING_MODEL=text-embedding-nomic-embed-text-v1.5@q8_0
EMBEDDING_DIM=768
# Ollama alternative: EMBEDDING_BASE_URL=http://localhost:11434/v1, EMBEDDING_MODEL=nomic-embed-text
```

Then add those keys to the `env` block in `deploy/helm/ro-builder/values.local.yaml`. A pod cannot reach the embedder
via `localhost`; point `EMBEDDING_BASE_URL` at the host address the cluster can route to (on docker-desktop k8s,
`http://host.docker.internal:1234/v1`). The optional retrieval knobs (`EMBEDDING_SIMILAR_MAX_DISTANCE`,
`EMBEDDING_HNSW_*`) are documented in the root `README.md`. The bundled `pgvector/pgvector:pg17` Postgres image already
has the extension available; the bootstrap creates the vector column and HNSW index on first startup when
`EMBEDDING_BASE_URL` is set.
