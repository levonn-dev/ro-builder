# ro-builder

LLM-orchestrated build generator for **Ragnarok Online**. Given a class, server,
playstyle, and a free-form description of what you want, it returns one or more **build trajectories**: ordered chains
of stat / skill / gear checkpoints from early game through max-level endgame, with deterministic combat numbers
attached at the load-bearing checkpoints.

The LLM proposes builds and reasons over results; a deterministic calc backend does the math. The split is deliberate:
RO has too many special cases (ASPD breakpoints, card stacking, size / element / race mods, refinement ATK,
hard-def vs soft-def) for an LLM to compute reliably; it will produce plausible-sounding numbers that don't match
in-game. The orchestrator feeds the calc output to the LLM as a structured breakdown (offense, defense, sustain,
utility, requirements_met, scenario_fit, uncertainty flags) rather than collapsing to a single score, so the model
reasons over the same shape a human would.

The calc backend, the LLM provider, and the server profile are all pluggable. The repo ships
a [rocalc](https://rocalc.com) adapter, Anthropic + OpenAI-compatible providers, and a UARO server profile as defaults;
each is replaceable without touching the surrounding code (see [Contributing & extending](#contributing--extending)).

Pre-renewal is the only mode shipped today (the calc adapter hasn't been validated for renewal), but the data layer
and API surface are mode-aware, so renewal is a backend swap, not an architecture change. The project is most useful
on private servers, where rates / custom items / class rules vary widely and automated build exploration earns its
keep, but nothing in the architecture is private-server-specific; iRO and other officials work with a server profile
authored for them.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  Go API  (cmd/api)                                           │
│  ┌──────────────┐   ┌──────────────────────────────────────┐ │
│  │ HTTP handler │ → │ Orchestrator                         │ │
│  └──────────────┘   │   ↪ LLM provider (Claude / OpenAI)   │ │
│                     │   ↪ Tool registry                    │ │
│                     │   ↪ Build library (SQLite)           │ │
│                     │   ↪ Scoring client → ─────┐          │ │
│                     └───────────────────────────│──────────┘ │
└─────────────────────────────────────────────────│────────────┘
                                                  │ HTTP /score
                                                  ▼
                ┌─────────────────────────────────────────────┐
                │ Calc Sidecar  (calc-sidecar/server.ts)      │
                │   ShimPool of N workers, each holding its   │
                │   own JSDOM window + calc-backend globals.  │
                └─────────────────────────────────────────────┘
```

Two deployable units. Both can be run natively, via `docker compose`, or via Helm/Tilt on Kubernetes.

Directory map:

```
cmd/                  one Go binary per directory
  api/                  HTTP server
  build-catalog/        emulator db → embedded JSON (one-shot)
  build-rocalc-mapping/ rocalc-id ↔ iRO-id table (one-shot)
  enrich-mapping-rms/   ratemyserver.net lookup helper
  verify-mapping/       spot-check tool
internal/
  api/                  HTTP handlers, OpenAPI types
  buildlibrary/         SQLite persistence
  catalog/              //go:embed item/mob/skill data
  data/                 emulator-source parsers
    sources/hercules/
    sources/rathena/
  domain/               pure types: ServerProfile, Trajectory, Snapshot...
  llm/                  provider interface + registry
    anthropic/
    openai/
    tools/              LLM-facing tools (lookup_item, score_build, ...)
  orchestrator/         request → LLM loop → response
  scoring/              sidecar client, score result types, gates
calc-sidecar/
  server.ts             POST /score
  src/
    types.ts            public contract (Go side mirrors this)
    shim.ts             re-exports active backend via the registry
    pool.ts             worker-thread pool
    backends/
      registry.ts       backend table + CALC_BACKEND dispatch
      rocalc/           rocalc adapter (form-template, mapping.json)
      stub/             deterministic fiction (CI / non-calc dev)
  vendor/<engine>/      ← you drop the backend's source files here (gitignored;
                          parent dir tracked via .gitkeep)
configs/servers/        one YAML per server profile (embedded)
deploy/                 Helm chart + Tilt + sealed-secrets
scripts/                e2e.sh, generate.sh, docker-e2e.sh, ...
api/openapi.yaml        the public HTTP contract
```

---

## Capabilities

What's wired up today:

| Aspect       | What's shipped                                                                           |
|--------------|------------------------------------------------------------------------------------------|
| Mode         | Pre-renewal only today; `renewal` is rejected at the API boundary                        |
| Calc backend | rocalc adapter (Node + jsdom sidecar); contract is engine-agnostic                       |
| Classes      | 45 of 46 pre-renewal classes (High Novice / id 34 excluded); prompt-tuned on Taekwon Kid |
| Servers      | UARO profile shipped; alternates by dropping a YAML under `configs/servers/`             |
| Playstyle    | PvM tuned; `pvp` / `balanced_*` accepted but less validated                              |
| LLM provider | Anthropic (default) and OpenAI-compatible (LM Studio, llama.cpp, Ollama, vLLM, ...)      |
| Persistence  | SQLite at `BUILDLIBRARY_PATH`                                                            |

---

## Prerequisites

- **Go** 1.26.2+ (see `.go-version`)
- **Node** 24+ (uses Node 24's TypeScript strip mode; no compile step)
- **jq** (used by `scripts/e2e.sh` and the curl examples below)
- **[go-task](https://taskfile.dev)** (`task` runner; install via
  `go install github.com/go-task/task/v3/cmd/task@latest` or your package manager)
- **golangci-lint** for `task lint`

Optional:

- **Docker / docker compose** for `task docker:up`
- **Helm 3 + Tilt + kubectl** for k8s dev via `task tilt:up`
- **A pre-renewal emulator checkout** (Hercules or rAthena) only needed if you want to regenerate the embedded catalog
  yourself. A pre-built `internal/catalog/data/catalog.json` is already committed.

---

## Setup

### 1. Clone and bootstrap

```bash
git clone <this-repo> ro-builder
cd ro-builder
task bootstrap     # copies .env.example → .env, go mod download, npm install
```

Then edit `.env` and set `LLM_API_KEY` (Anthropic by default; switch provider via `LLM_PROVIDER` / `LLM_BASE_URL`). The
API still starts and serves `/score` without a key; `/generate` returns 503 (`LLM provider not configured`) until a
key is set.

### 2. Provide files for the calc backend

The calc backend's source files are **not bundled**. The default adapter is rocalc; drop its JS files into
`calc-sidecar/vendor/rocalc/`. The expected snapshot date is **2026-04-06** (pinned in the adapter's file list).

```bash
mkdir -p calc-sidecar/vendor/rocalc
cd calc-sidecar/vendor/rocalc

DATE=2026-04-06
for f in skill head item etc monster card foot; do
  curl -fsSLO "https://rocalc.com/${f}_${DATE}.js"
done

# Page chrome; not loaded by the sidecar, but useful if you want to open
# the vendored copy in a browser for cross-checking.
curl -fsSLO https://rocalc.com/index.html
curl -fsSLO https://rocalc.com/style.css

cd -
```

Notes specific to the rocalc adapter:

- For a different snapshot date, update the `ROCALC_FILES` list in [
  `calc-sidecar/src/backends/rocalc/index.ts`](calc-sidecar/src/backends/rocalc/index.ts) to match the files you
  downloaded.
- The rocalc-id ↔ iRO-id mapping at `calc-sidecar/src/backends/rocalc/mapping.json` may drift across snapshots;
  regenerate via `task mapping` (requires an emulator checkout;
  see [regenerating the catalog](#regenerating-the-catalog) below).
- Some skill / damage formulas are inexact (left-hand crit on Assassins, Soul Breaker / Shield Boomerang / Shield
  Chain / Grimtooth, enemy Grand Cross / Evil Land). The adapter surfaces these through `uncertainty.flags` on the score
  result; the orchestrator passes them
  to the LLM.
- Never modify files under `vendor/`. All glue lives in the shim.

Alternatively, set `CALC_BACKEND=stub` to run the sidecar against the built-in stub backend. It skips vendor entirely
and returns deterministic fiction numbers. Useful for iterating on shim plumbing, the HTTP layer, or the API
without rocalc files on disk. Not useful for any real build calculation.

To use a different calc backend, see [Adding a calc backend](#adding-a-calc-backend).

### 3. Verify with the smoke test

Each e2e task boots the sidecar + API in an isolated environment, sends a `/score` request and (if `LLM_API_KEY` is
set) a full `/generate` cycle, validates the responses, and tears down. Exit 0 means the pipeline is wired. Pick the
flavor that matches how you intend to deploy; or run all three before merging anything that touches packaging.

```bash
task e2e          # native; boots Node + Go on isolated ports against a temp DB. Fast.
task docker:e2e   # docker compose; verifies the shipped images, env wiring, healthchecks. Slower.
task tilt:e2e     # kubernetes; helm install in a temp namespace, helm uninstall on exit. Slowest.
```

---

## Running

Two services. They run independently; pick one of the three orchestration paths:

### Native (fastest dev loop)

```bash
# Terminal A: calc sidecar (port 7401)
task run:sidecar

# Terminal B: Go API (port 8080)
task run:api
```

### docker compose (prod-shaped local)

```bash
task docker:up      # builds both images, runs in the foreground
task docker:down    # stops; named volume `ro-builder-buildlibrary` survives
task docker:nuke    # stops AND drops the buildlibrary volume
```

The sidecar's calc backend defaults to rocalc. Set `CALC_BACKEND=stub` in your shell env (or `.env`) before
`task docker:up` to use the stub backend instead.

### Kubernetes via Tilt (live-reload k8s)

```bash
task tilt:bootstrap   # one-shot: install sealed-secrets controller, create namespace
task tilt:seal        # encrypt LLM_API_KEY from .env into a SealedSecret
task tilt:up          # start the full stack under Tilt (Ctrl-C to stop)
```

The chart's `sidecar.calcBackend` value defaults to `rocalc`. Override via `values.local.yaml` or `--set` for
stub-backed local runs.

---

## API

Full OpenAPI 3.0 contract: [`api/openapi.yaml`](api/openapi.yaml). Bruno collection: [`bruno/`](bruno/).

| Method | Path                | Purpose                                                                  |
|--------|---------------------|--------------------------------------------------------------------------|
| POST   | `/score`            | Direct calc; fully-specified Build → derived stats + optional combat sim |
| POST   | `/generate`         | LLM-driven; enqueue a generation job; returns `202 + {id}`               |
| GET    | `/generations/{id}` | Poll status of an enqueued generation                                    |
| GET    | `/builds`           | Paginated list of saved trajectories                                     |
| GET    | `/builds/{id}`      | One full saved trajectory with item / skill names resolved               |

### Score a build directly

```bash
curl -s http://localhost:8080/score \
  -H 'content-type: application/json' \
  -d '{
    "build": {
      "class": "swordsman",
      "level": {"base": 50, "job": 30},
      "stats": {"str": 60, "agi": 30, "vit": 40, "int": 1, "dex": 30, "luk": 1}
    }
  }' | jq .derived
```

### Generate a build (async)

```bash
# 1. Enqueue
ID=$(curl -s http://localhost:8080/generate \
  -H 'content-type: application/json' \
  -d '{
    "class": "taekwon_kid",
    "server": "uaro",
    "playstyle": "pvm",
    "description": "viable PvM ranker to 99/50, friendly to a fresh player"
  }' | jq -r .id)

# 2. Poll (cadence: ~30s recommended)
while :; do
  STATUS=$(curl -s "http://localhost:8080/generations/$ID" | jq -r .status)
  echo "$STATUS"
  [[ "$STATUS" == "completed" || "$STATUS" == "failed" ]] && break
  sleep 30
done

# 3. Fetch enriched build
curl -s "http://localhost:8080/builds/$ID" | jq .
```

Or use the convenience wrapper:

```bash
scripts/generate.sh                    # default request body
scripts/generate.sh path/to/body.json  # custom body
```

---

## Contributing & extending

Five extension points, all additive; no refactors required.

### Adding an LLM provider

1. Create `internal/llm/<name>/client.go`:

   ```go
   package myprovider

   import "github.com/levonn-dev/ro-builder/internal/llm"

   func init() {
       llm.Register("myprovider", func(c llm.Config) (llm.Provider, error) {
           return &client{cfg: c}, nil
       })
   }

   type client struct{ cfg llm.Config }

   func (c *client) Complete(ctx context.Context, system string,
       messages []llm.Message, tools []llm.Tool) (llm.Response, error) {
       // translate to/from your wire format; preserve tool_use / tool_result blocks
   }
   ```

2. Side-effect-import it from [`cmd/api/main.go`](cmd/api/main.go):

   ```go
   import _ "github.com/levonn-dev/ro-builder/internal/llm/myprovider"
   ```

3. Set `LLM_PROVIDER=myprovider` in `.env`.

The contract is [`internal/llm/provider.go`](internal/llm/provider.go); it mirrors a messages-with-tools shape because
that's what the orchestrator iteration loop expects. The OpenAI provider doubles as a working example of adapting a
different wire format and also covers LM Studio, llama.cpp, Ollama, Azure OpenAI, OpenRouter, and vLLM via
`LLM_BASE_URL`.

### Adding a calc backend

The shim contract is in [`calc-sidecar/src/types.ts`](calc-sidecar/src/types.ts); `ShimSession`, `ScoreRequest`,
`ScoreResponse`, slot keys, derived-stats shape. The Go side mirrors it.

1. Create `calc-sidecar/src/backends/<engine>/index.ts` exporting `createShim(): ShimSession` and
   `CALC_VERSION: string`. Both must load synchronously: the registry pulls the selected backend in with
   `require()`, so the backend's module graph must not use top-level `await`, and `createShim` stays
   synchronous. (A worker whose module graph is async can abort the process if it's terminated mid-load; see
   the note in `registry.ts`.)
2. Put engine-specific assets (synthetic HTML form, ID-mapping JSON, etc.) alongside it.
3. Drop the engine's source files under `calc-sidecar/vendor/<engine>/`.
4. Register your backend in [`calc-sidecar/src/backends/registry.ts`](calc-sidecar/src/backends/registry.ts) by adding one
   line in the `BACKENDS` table (a module specifier; the registry loads only the selected backend, synchronously
   via `require()`):

   ```ts
   const BACKENDS: Record<string, string> = {
     rocalc: "./rocalc/index.ts",
     stub: "./stub/index.ts",
     <engine>: "./<engine>/index.ts",  // ← your backend
   };
   ```

5. Select it at runtime via `CALC_BACKEND=<engine>` (env var; default `rocalc`).
6. Write a boundary translation if your engine doesn't speak iRO IDs. The rocalc adapter keeps a `mapping.json` and
   translates per call; see [`calc-sidecar/src/backends/rocalc/index.ts`](calc-sidecar/src/backends/rocalc/index.ts).

### Adding a server profile

1. Drop a new file at `configs/servers/<key>.yaml`. The schema is `domain.ServerProfile` (see [
   `internal/domain/server.go`](internal/domain/server.go)); rates, caps, class-change rules, item-change rules,
   leveling content, custom mobs, custom items, quality-gate defaults.
2. Cite the source URL in a comment above each section you change; wikis drift.
3. Rebuild; the directory is `//go:embed`'d into the binary; no runtime reload.
4. Pass the key as `server` in the `/generate` request.

[`configs/servers/uaro.yaml`](configs/servers/uaro.yaml) is the reference. `ServerProfile.CustomMobs` carries
server-specific overlays (e.g. UARO's Old Glast Heim).

### Adding an LLM tool

1. Create `internal/llm/tools/<name>.go` implementing `tools.Tool` (`Definition()` for the JSON schema, `Execute()` for
   dispatch). See [`internal/llm/tools/lookup_item.go`](internal/llm/tools/lookup_item.go) for the simplest shape.
2. Register it where the orchestrator wires the registry; search `tools.NewRegistry` in [
   `cmd/api/main.go`](cmd/api/main.go).
3. Write tests alongside; every shipped tool has a `*_test.go`.

Tools should be stateless across calls; pass any dependency (scoring client, catalog) in at construction.

### Adding an emulator data source

`internal/data/sources/<name>/` for a new emulator parser. Hercules (libconfig) and rAthena (YAML) are present; either
produces the same iRO-IDed catalog. Mode-aware via `data.Mode` (`PreRenewal` | `Renewal`).

### Regenerating the catalog

The catalog (items, mobs, skills, cards) is generated from a Hercules or rAthena checkout; both ship pre-renewal item
DBs with iRO IDs.

```bash
# Default: Hercules at sibling path ../Hercules
git clone https://github.com/HerculesWS/Hercules.git ../Hercules
task catalog

# Alternate: rAthena
git clone https://github.com/rathena/rathena.git ../rathena
task catalog -- -source rathena
```

The generated `internal/catalog/data/catalog.json` is committed (so first-time users don't need the emulator checkout).
Re-run when upstream data shifts.

The rocalc-id ↔ iRO-id mapping is generated separately:

```bash
task mapping            # rebuild calc-sidecar/src/backends/rocalc/mapping.json
task mapping:verify     # cross-check against Hercules + rAthena
task mapping:enrich     # query ratemyserver.net for unmatched entries
```

### Code style

- `task fmt` before committing; Go (`gofmt`) and TypeScript (`prettier`).
- `task check` runs the pre-commit gate: fmt-check, lint, build, test, helm lint.
- `task check:full` runs everything plus all three e2e flavors (native, compose, k8s).
- Comments: only when WHY is non-obvious. API handlers in `internal/api/` are an explicit exception; those carry
  reference-doc comments. Everywhere else, identifier names do the explaining.

### CI

Every PR and push to `main` runs the CI workflow at [`.github/workflows/ci.yml`](.github/workflows/ci.yml). The
workflow mirrors `task check` (fmt-check, lint, build, unit tests, helm lint) plus native and docker-compose e2e
runs, all against the stub calc backend (no `vendor/rocalc/` needed). `LLM_API_KEY` is not provisioned in CI; the e2e
suite verifies `/generate` returns 503 (`LLM provider not configured`) in that mode rather than running the
happy-path LLM loop.

---

## TODO

Not yet done, roughly in priority order:

- [x] Add self-buffs to calc
- [ ] Add self-buffs to scoring
- [x] Include skill usage/damage in calc and scoring
- [ ] Vector search over saved trajectories' reasoning text. Today's `get_similar_past_builds` uses class+scenario
  lookup.
- [ ] Add RAG with injecting get_similar_past_builds into initial system prompt
- [ ] Postgres migration for the build library. SQLite pins the API to `replicas: 1` and `strategy: Recreate`; HPA / PDB
  are excluded from the Helm chart until this lands.
- [ ] General prompt tuning - less tokens, more progress.
- [ ] More golden fixtures beyond Taekwon Kid. The shim handles all 45 supported classes already
- [ ] Golden fixtures for Full Support builds
- [ ] Renewal mode end-to-end. The API rejects `mode: "renewal"` at the boundary today; the data layer is already
  mode-aware. The calc adapter hasn't been validated for renewal.
- [ ] Rotation simulator with SP economy. Today's scoring uses single-skill DPS + sustained auto-attack as a proxy.
- [ ] HP pot economy in scoring. SP sustain matters more in practice today.
- [ ] Multi-target AoE scoring. One target per scored snapshot today.
- [ ] Real time-to-level scoring at leveling checkpoints. Currently the leveling target is attached as a proxy for "can
  you farm here"
- [ ] Genetic / beam-search optimizer over gear combinations. Today the LLM proposes; deterministic scoring ranks.
- [ ] Custom dungeons (containers) in `ServerProfile`. Custom mobs are wired; containers are not.
- [ ] Model custom skill changes from server profile into calc
- [ ] Model custom equipment changes from server profile into calc

Bug reports, missing card effects, server-profile additions, and additional calc backends are all welcome via PR.
