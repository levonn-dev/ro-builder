#!/usr/bin/env bash
# e2e.sh; orchestrate sidecar + API in an isolated environment, smoke-
# test the public endpoints with strict response validation, and tear
# everything down.
#
# Isolation: ports, build-library DB, and process logs all land in a
# per-run tempdir. Nothing under the repo's normal paths is touched, so
# parallel runs (and an already-running dev sidecar/api) don't conflict.
#
# Validation philosophy: errors are NOT acceptable. A 200 with an
# {"error":"..."} body is still a failure. Beyond "did the request
# succeed," each endpoint is checked for the structural shape and a
# plausible-content signal proving the full pipeline ran:
#
#   /score   ; derived stats object with maxHp > 0, aspd > 0, atk.base
#               present. Proves: sidecar reachable, shim init OK, rocalc
#               recompute ran, response decoded.
#
#   /generate; async: POST → 202 + id → poll /generations/{id} →
#               GET /builds/{id}. Validates: class matches request,
#               primary.snapshots non-empty, ≥1 snapshot scored, ≥1
#               snapshot has gates, gate_summary.pass > 0. Proves the
#               full pipeline; orchestrator → LLM loop →
#               submit_trajectory → canonical re-scoring → saved.
#               Skipped when LLM_API_KEY is unset.
#
# Exit code: 0 on success, non-zero on first failure. Process logs and
# response JSON land in the tempdir and are dumped on failure for
# triage.
#
# The /generate flow is asynchronous:
#   1. POST /generate → 202 + {id}
#   2. Poll GET /generations/{id} until completed or failed
#   3. GET /builds/{id} for the enriched result
#   Polling: 10s interval, 900s cap.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Load .env so direct `bash scripts/e2e.sh` sees the same vars as
# `task e2e` (which loads .env via Taskfile's dotenv directive). Doesn't
# clobber vars already set in the calling shell; those win.
if [[ -f .env ]]; then
	set -o allexport
	# shellcheck disable=SC1091
	source .env
	set +o allexport
fi

if ! command -v jq >/dev/null 2>&1; then
	echo "[e2e] jq not installed; required for response validation" >&2
	echo "      ubuntu/debian: apt-get install jq" >&2
	echo "      macos:        brew install jq" >&2
	exit 2
fi

TMP="$(mktemp -d -t ro-builder-e2e.XXXXXX)"

cleanup() {
	local code=$?
	if [[ -n "${API_PID:-}" ]]; then
		kill "$API_PID" 2>/dev/null || true
	fi
	if [[ -n "${SIDECAR_PID:-}" ]]; then
		kill "$SIDECAR_PID" 2>/dev/null || true
	fi
	wait 2>/dev/null || true
	docker rm -f "${E2E_PG_NAME:-}" >/dev/null 2>&1 || true
	if (( code != 0 )); then
		echo
		echo "[e2e] FAILED (exit $code); diagnostics follow:"
		for f in score-response.json enqueue-response.json gen-status.json build-response.json sidecar.log api.log; do
			if [[ -f "$TMP/$f" ]]; then
				echo "--- $f (tail) ---"
				tail -40 "$TMP/$f"
				echo
			fi
		done
	fi
	rm -rf "$TMP"
}
trap cleanup EXIT

# Pick non-default ports so a dev sidecar/api running on the standard
# 7401/8080 doesn't fight this script.
E2E_SIDECAR_PORT=17401
E2E_API_PORT=18080
SIDECAR_URL="http://localhost:$E2E_SIDECAR_PORT"
API_URL="http://localhost:$E2E_API_PORT"

E2E_PG_NAME="ro-builder-e2e-pg-$$"
E2E_PG_PORT=15432
docker run -d --rm --name "$E2E_PG_NAME" \
	-e POSTGRES_USER=robuilder -e POSTGRES_PASSWORD=robuilder -e POSTGRES_DB=robuilder \
	-p "$E2E_PG_PORT:5432" pgvector/pgvector:pg17 >/dev/null
E2E_DATABASE_URL="postgres://robuilder:robuilder@localhost:$E2E_PG_PORT/robuilder?sslmode=disable"
for _ in $(seq 1 60); do
	if docker exec "$E2E_PG_NAME" pg_isready -U robuilder -d robuilder >/dev/null 2>&1; then break; fi
	sleep 0.5
done
if ! docker exec "$E2E_PG_NAME" pg_isready -U robuilder -d robuilder >/dev/null 2>&1; then
	echo "[e2e] postgres did not become ready within 30s" >&2
	exit 1
fi

echo "[e2e] tempdir: $TMP"
echo "[e2e] sidecar :$E2E_SIDECAR_PORT  api :$E2E_API_PORT  pg localhost:$E2E_PG_PORT"

# --- sidecar ---
echo "[e2e] starting sidecar"
(
	cd calc-sidecar
	PORT=$E2E_SIDECAR_PORT exec node server.ts
) >"$TMP/sidecar.log" 2>&1 &
SIDECAR_PID=$!

for _ in $(seq 1 60); do
	if ! kill -0 "$SIDECAR_PID" 2>/dev/null; then
		echo "[e2e] sidecar process exited before becoming ready; check sidecar.log" >&2
		exit 1
	fi
	if curl -sf -o /dev/null -X POST "$SIDECAR_URL/score" \
		-H 'content-type: application/json' -d '{}' 2>/dev/null; then
		break
	fi
	sleep 0.5
done
if ! curl -sf -o /dev/null -X POST "$SIDECAR_URL/score" \
	-H 'content-type: application/json' -d '{}' 2>/dev/null; then
	echo "[e2e] sidecar did not become ready within 30s" >&2
	exit 1
fi
echo "[e2e] sidecar ready"

# --- api ---
#
# Build the binary first instead of `go run ./cmd/api`. `go run` spawns
# a child binary (/tmp/go-build.../exe/api) and tracks the build wrapper
# in $!; `kill $API_PID` then takes out the wrapper but leaves the
# child squatting on $E2E_API_PORT, breaking the next run. Building +
# exec'ing the binary in a subshell means $API_PID is the binary itself,
# so cleanup actually works.
echo "[e2e] building api"
go build -o "$TMP/api" ./cmd/api >"$TMP/api-build.log" 2>&1 || {
	echo "[e2e] api build failed" >&2
	tail -40 "$TMP/api-build.log" >&2
	exit 1
}

echo "[e2e] starting api"
(
	ADDR=":$E2E_API_PORT" \
		SIDECAR_URL="$SIDECAR_URL" \
		DATABASE_URL="$E2E_DATABASE_URL" \
		exec "$TMP/api"
) >"$TMP/api.log" 2>&1 &
API_PID=$!

for _ in $(seq 1 120); do
	if ! kill -0 "$API_PID" 2>/dev/null; then
		echo "[e2e] api process exited before becoming ready; check api.log" >&2
		exit 1
	fi
	if curl -sf -o /dev/null -X POST "$API_URL/score" \
		-H 'content-type: application/json' -d '{}' 2>/dev/null; then
		break
	fi
	sleep 0.5
done
if ! curl -sf -o /dev/null -X POST "$API_URL/score" \
	-H 'content-type: application/json' -d '{}' 2>/dev/null; then
	echo "[e2e] api did not become ready within 60s" >&2
	exit 1
fi
echo "[e2e] api ready"

# --- /score validation ---
#
# The API's /score request shape is {build, scenario?}; the build itself
# is what carries class / level / stats / equipment. Sending a flat
# (sidecar-style) request makes the API silently decode to a zero-value
# Build and the calc returns lvl-1-Novice defaults; that's how a
# malformed request can look "successful" with default-shaped numbers.
# The bounds below are intentionally above what a default Build would
# produce so a shape regression fails the validation.
echo "[e2e] POST /score"
SCORE_BODY='{
	"build": {
		"class": "swordsman",
		"level": {"base": 50, "job": 30},
		"stats": {"str": 60, "agi": 30, "vit": 40, "int": 1, "dex": 30, "luk": 1}
	}
}'

# Capture both body and HTTP status so we can distinguish transport
# errors (curl -sf would have already exited) from in-body error
# objects (HTTP 200 with {"error":"..."}).
HTTP_CODE=$(curl -s -o "$TMP/score-response.json" -w '%{http_code}' \
	-X POST "$API_URL/score" \
	-H 'content-type: application/json' \
	-d "$SCORE_BODY")
if [[ "$HTTP_CODE" != "200" ]]; then
	echo "[e2e] /score returned HTTP $HTTP_CODE (expected 200)" >&2
	exit 1
fi

# Validation contract:
#   - No top-level "error" key (would mean a 200-with-error-body)
#   - .derived is an object
#   - Backend-dependent numeric floors. Read calc_version to pick:
#     - stub backend ("stub-v1"): returns fiction regardless of input;
#       only shape + positive-bounds invariants are checkable.
#     - real backend (e.g. rocalc): floors above the lvl-1-Novice
#       defaults (maxHp~40) catch request-shape regressions where the
#       API silently decoded to a zero-value Build.
CALC_VERSION=$(jq -r '.calc_version // ""' "$TMP/score-response.json")
if [[ "$CALC_VERSION" == stub-* ]]; then
	BOUNDS_FILTER='(.derived.maxHp > 0) and (.derived.aspd > 0) and (.derived.atk.base >= 0) and (.derived.hit >= 0)'
	BOUNDS_DESC='derived.{maxHp>0, aspd>0, atk.base>=0, hit>=0} (stub mode; no shape-regression detection)'
else
	BOUNDS_FILTER='(.derived.maxHp > 1000) and (.derived.aspd > 100) and (.derived.atk.base > 0) and (.derived.hit > 50)'
	BOUNDS_DESC='derived.{maxHp>1000, aspd>100, atk.base>0, hit>50} (defaults would yield maxHp~40; a shape regression would trip this)'
fi
if ! jq -e "
	(.error // null) == null
	and (.derived | type == \"object\")
	and ($BOUNDS_FILTER)
" "$TMP/score-response.json" >/dev/null 2>&1; then
	echo "[e2e] /score response failed validation" >&2
	echo "      expected: no error; $BOUNDS_DESC" >&2
	exit 1
fi
SCORE_HP=$(jq -r '.derived.maxHp' "$TMP/score-response.json")
SCORE_ASPD=$(jq -r '.derived.aspd' "$TMP/score-response.json")
SCORE_HIT=$(jq -r '.derived.hit' "$TMP/score-response.json")
echo "[e2e] /score OK (calc=$CALC_VERSION maxHp=$SCORE_HP aspd=$SCORE_ASPD hit=$SCORE_HIT)"

# --- /generate validation (conditional) ---
if [[ -n "${LLM_API_KEY:-}" ]]; then
	echo "[e2e] LLM_API_KEY set; POST /generate (async)"
	GEN_BODY='{
		"class": "taekwon_kid",
		"server": "uaro",
		"playstyle": "pvm",
		"description": "e2e smoke test; produce a viable PvM build to lvl 99/50"
	}'

	# Step 1: enqueue; expect 202 + {id}.
	HTTP_CODE=$(curl -s -o "$TMP/enqueue-response.json" -w '%{http_code}' \
		--max-time 30 \
		-X POST "$API_URL/generate" \
		-H 'content-type: application/json' \
		-d "$GEN_BODY")
	if [[ "$HTTP_CODE" != "202" ]]; then
		echo "[e2e] /generate returned HTTP $HTTP_CODE (expected 202)" >&2
		exit 1
	fi
	GEN_ID="$(jq -r '.id' "$TMP/enqueue-response.json")"
	if [[ -z "$GEN_ID" || "$GEN_ID" == "null" ]]; then
		echo "[e2e] /generate 202 body missing .id" >&2
		exit 1
	fi
	echo "[e2e] /generate enqueued: $GEN_ID"

	# Step 2: poll /generations/{id} until completed or failed.
	POLL_INTERVAL=10   # seconds
	MAX_WAIT_SECONDS=900
	GEN_START=$(date +%s)
	GEN_LAST_STATUS=""
	while :; do
		if ! curl -sf -o "$TMP/gen-status.json" "$API_URL/generations/$GEN_ID"; then
			echo "[e2e] poll /generations/$GEN_ID failed" >&2
			exit 1
		fi
		GEN_STATUS="$(jq -r '.status' "$TMP/gen-status.json")"
		if [[ "$GEN_STATUS" != "$GEN_LAST_STATUS" ]]; then
			echo "[e2e] generation status: $GEN_STATUS"
			GEN_LAST_STATUS="$GEN_STATUS"
		fi
		case "$GEN_STATUS" in
			completed)
				break
				;;
			failed)
				GEN_ERR="$(jq -r '.error // "(no error)"' "$TMP/gen-status.json")"
				echo "[e2e] generation failed: $GEN_ERR" >&2
				exit 1
				;;
			queued|running)
				NOW=$(date +%s)
				ELAPSED=$((NOW - GEN_START))
				if (( ELAPSED >= MAX_WAIT_SECONDS )); then
					echo "[e2e] /generate timed out after ${MAX_WAIT_SECONDS}s" >&2
					exit 1
				fi
				sleep "$POLL_INTERVAL"
				;;
			*)
				echo "[e2e] unexpected generation status: $GEN_STATUS" >&2
				exit 1
				;;
		esac
	done

	# Step 3: fetch enriched build from /builds/{id}.
	BUILD_ID="$GEN_ID"
	if ! curl -sf -o "$TMP/build-response.json" "$API_URL/builds/$BUILD_ID"; then
		echo "[e2e] GET /builds/$BUILD_ID failed" >&2
		exit 1
	fi

	# Validation contract for the enriched build:
	#   - No top-level "error" key
	#   - .class equals the request's class (SavedTrajectory root field)
	#   - .primary.snapshots is a non-empty array
	#   - At least one snapshot has .score != null (canonical scoring
	#     ran; proves orchestrator → sidecar → scoring wiring fires
	#     end-to-end, not just LLM-self-reported numbers)
	#   - At least one snapshot has gates evaluated (proves the gates
	#     evaluator ran on canonically-scored output)
	#   - .gate_summary.pass > 0 (at least something passed quality gates)
	if ! jq -e --arg class "taekwon_kid" '
		(.error // null) == null
		and (.class == $class)
		and (.primary.snapshots | type == "array")
		and (.primary.snapshots | length >= 1)
		and ([.primary.snapshots[] | select(.score != null)] | length >= 1)
		and ([.primary.snapshots[] | select(.gates != null and (.gates | length) > 0)] | length >= 1)
		and (.gate_summary.pass > 0)
	' "$TMP/build-response.json" >/dev/null 2>&1; then
		echo "[e2e] /builds/$BUILD_ID response failed validation" >&2
		echo "      expected: no error; class==taekwon_kid;" >&2
		echo "                ≥1 snapshot with .score; ≥1 snapshot with gates;" >&2
		echo "                gate_summary.pass > 0" >&2
		exit 1
	fi
	GEN_SNAP_COUNT=$(jq -r '.primary.snapshots | length' "$TMP/build-response.json")
	GEN_SCORED_COUNT=$(jq -r '[.primary.snapshots[] | select(.score != null)] | length' "$TMP/build-response.json")
	GEN_GATED_COUNT=$(jq -r '[.primary.snapshots[] | select(.gates != null and (.gates | length) > 0)] | length' "$TMP/build-response.json")
	GEN_GATE_PASS=$(jq -r '.gate_summary.pass' "$TMP/build-response.json")
	echo "[e2e] /generate OK (id=$GEN_ID snapshots=$GEN_SNAP_COUNT scored=$GEN_SCORED_COUNT gated=$GEN_GATED_COUNT gate_pass=$GEN_GATE_PASS)"
else
	# LLM_API_KEY unset: API starts in score-only mode; /generate must
	# return 503 with "LLM provider not configured" rather than crashing
	# or accepting a job that can't be processed. Verify rather than skip.
	echo "[e2e] LLM_API_KEY not set; verifying /generate returns 503"
	HTTP_CODE=$(curl -s -o "$TMP/generate-503.json" -w '%{http_code}' \
		--max-time 10 \
		-X POST "$API_URL/generate" \
		-H 'content-type: application/json' \
		-d '{"class":"taekwon_kid","server":"uaro","playstyle":"pvm","description":"503 probe"}')
	if [[ "$HTTP_CODE" != "503" ]]; then
		echo "[e2e] /generate returned HTTP $HTTP_CODE (expected 503 when LLM_API_KEY unset)" >&2
		cat "$TMP/generate-503.json" >&2 || true
		exit 1
	fi
	if ! jq -e '(.error // "") | contains("LLM provider not configured")' "$TMP/generate-503.json" >/dev/null 2>&1; then
		echo "[e2e] /generate 503 body missing 'LLM provider not configured'" >&2
		cat "$TMP/generate-503.json" >&2 || true
		exit 1
	fi
	echo "[e2e] /generate 503 confirmed (score-only mode)"
fi

echo "[e2e] all checks passed"
