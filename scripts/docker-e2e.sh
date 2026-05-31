#!/usr/bin/env bash
# docker-e2e.sh; run the same end-to-end validation as scripts/e2e.sh
# but against `docker compose up` instead of natively-launched processes.
#
# Purpose: catch packaging bugs that the native e2e can't see. The native
# script verifies application logic against fast-rebuild local binaries;
# this one verifies the actual Docker images that ship; Dockerfile
# correctness, COPY paths, env-var wiring, healthcheck behavior, volume
# mounts, network names.
#
# Isolation: a unique COMPOSE_PROJECT_NAME and randomized host port keep
# this script from conflicting with a developer's `task docker:up` (or
# concurrent runs of the script itself). The named volume is dropped on
# cleanup so leftover state doesn't bleed into the next run.
#
# Validation: same shape and contracts as scripts/e2e.sh; same /score
# bounds, same /generate validation when LLM_API_KEY is set. The
# packaging is the variable; the assertions are constant.
#
# Exit code: 0 on success, non-zero on first failure. Container logs
# land in the tempdir and are dumped on failure for triage.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Load .env so LLM_API_KEY (and any other LLM_* overrides) flow into
# `docker compose up` the same way they do for the native script. Doesn't
# clobber vars already set in the calling shell.
if [[ -f .env ]]; then
	set -o allexport
	# shellcheck disable=SC1091
	source .env
	set +o allexport
fi

if ! command -v jq >/dev/null 2>&1; then
	echo "[docker-e2e] jq required" >&2
	exit 2
fi
if ! command -v docker >/dev/null 2>&1; then
	echo "[docker-e2e] docker required" >&2
	exit 2
fi

TMP="$(mktemp -d -t ro-builder-docker-e2e.XXXXXX)"

# Unique project name so containers / network / volume don't collide
# with a dev `task docker:up`. Random suffix protects against concurrent
# runs of this script.
export COMPOSE_PROJECT_NAME="ro-builder-e2e-$$-$RANDOM"
# High port range to avoid the dev default (8080).
export API_HOST_PORT="$(shuf -i 28000-29999 -n 1)"
API_URL="http://localhost:$API_HOST_PORT"

cleanup() {
	local code=$?
	if (( code != 0 )); then
		echo
		echo "[docker-e2e] FAILED (exit $code); diagnostics follow:"
		echo "--- compose ps ---"
		docker compose ps 2>&1 || true
		echo
		echo "--- sidecar logs (tail) ---"
		docker compose logs --tail 60 sidecar 2>&1 || true
		echo
		echo "--- api logs (tail) ---"
		docker compose logs --tail 60 api 2>&1 || true
		for f in score-response.json generate-response.json; do
			if [[ -f "$TMP/$f" ]]; then
				echo
				echo "--- $f ---"
				jq . "$TMP/$f" 2>/dev/null || cat "$TMP/$f"
			fi
		done
	fi
	# -v drops the named volume so the next run starts clean.
	docker compose down -v >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap cleanup EXIT

echo "[docker-e2e] tempdir:        $TMP"
echo "[docker-e2e] compose project: $COMPOSE_PROJECT_NAME"
echo "[docker-e2e] api host port:   $API_HOST_PORT"

# --- bring stack up ---
#
# --build forces image rebuild against the current source; the whole
# point of this script is to validate what *would* ship right now, not
# whatever stale image is cached. compose's depends_on + healthcheck
# means `up -d` blocks until the sidecar passes its readiness probe and
# the api starts.
echo "[docker-e2e] building + starting stack"
if ! docker compose up -d --build --wait >"$TMP/up.log" 2>&1; then
	echo "[docker-e2e] docker compose up failed:" >&2
	tail -40 "$TMP/up.log" >&2
	exit 1
fi
echo "[docker-e2e] stack ready"

# --- /score validation ---
#
# Same contract as scripts/e2e.sh; bounds chosen so a default-shape
# regression (e.g. wrong env wiring producing lvl-1 Novice numbers)
# trips the validation rather than masquerading as success.
echo "[docker-e2e] POST /score"
SCORE_BODY='{
	"build": {
		"class": "swordsman",
		"level": {"base": 50, "job": 30},
		"stats": {"str": 60, "agi": 30, "vit": 40, "int": 1, "dex": 30, "luk": 1}
	}
}'

HTTP_CODE=$(curl -s -o "$TMP/score-response.json" -w '%{http_code}' \
	-X POST "$API_URL/score" \
	-H 'content-type: application/json' \
	-d "$SCORE_BODY")
if [[ "$HTTP_CODE" != "200" ]]; then
	echo "[docker-e2e] /score returned HTTP $HTTP_CODE (expected 200)" >&2
	exit 1
fi

# Backend-dependent numeric floors. Stub returns fiction regardless of
# input, so only positive-bounds invariants are checkable. Real backends
# get the original shape-regression-catching floors (above lvl-1-Novice).
CALC_VERSION=$(jq -r '.calc_version // ""' "$TMP/score-response.json")
if [[ "$CALC_VERSION" == stub-* ]]; then
	BOUNDS_FILTER='(.derived.maxHp > 0) and (.derived.aspd > 0) and (.derived.atk.base >= 0) and (.derived.hit >= 0)'
	BOUNDS_DESC='derived.{maxHp>0, aspd>0, atk.base>=0, hit>=0} (stub mode)'
else
	BOUNDS_FILTER='(.derived.maxHp > 1000) and (.derived.aspd > 100) and (.derived.atk.base > 0) and (.derived.hit > 50)'
	BOUNDS_DESC='derived.{maxHp>1000, aspd>100, atk.base>0, hit>50} (a shape regression would yield Novice-default maxHp~40)'
fi
if ! jq -e "
	(.error // null) == null
	and (.derived | type == \"object\")
	and ($BOUNDS_FILTER)
	and (.calc_version | type == \"string\")
	and (.calc_version | length > 0)
" "$TMP/score-response.json" >/dev/null 2>&1; then
	echo "[docker-e2e] /score response failed validation" >&2
	echo "      expected: no error; $BOUNDS_DESC; non-empty calc_version" >&2
	exit 1
fi
SCORE_HP=$(jq -r '.derived.maxHp' "$TMP/score-response.json")
SCORE_ASPD=$(jq -r '.derived.aspd' "$TMP/score-response.json")
SCORE_HIT=$(jq -r '.derived.hit' "$TMP/score-response.json")
echo "[docker-e2e] /score OK (calc=$CALC_VERSION maxHp=$SCORE_HP aspd=$SCORE_ASPD hit=$SCORE_HIT)"

# --- /generate validation (conditional) ---
if [[ -n "${LLM_API_KEY:-}" ]]; then
	echo "[docker-e2e] LLM_API_KEY set; POST /generate"
	GEN_BODY='{
		"class": "taekwon_kid",
		"server": "uaro",
		"playstyle": "pvm",
		"description": "docker e2e smoke test; produce a viable PvM build to lvl 99/50"
	}'

	HTTP_CODE=$(curl -s -o "$TMP/generate-response.json" -w '%{http_code}' \
		--max-time 300 \
		-X POST "$API_URL/generate" \
		-H 'content-type: application/json' \
		-d "$GEN_BODY")
	if [[ "$HTTP_CODE" != "200" ]]; then
		echo "[docker-e2e] /generate returned HTTP $HTTP_CODE (expected 200)" >&2
		exit 1
	fi

	if ! jq -e --arg class "taekwon_kid" '
		(.error // null) == null
		and (.primary != null)
		and (.primary.class == $class)
		and (.primary.snapshots | type == "array")
		and (.primary.snapshots | length >= 1)
		and ([.primary.snapshots[] | select(.score != null)] | length >= 1)
		and ([.primary.snapshots[] | select(.gates != null and (.gates | length) > 0)] | length >= 1)
	' "$TMP/generate-response.json" >/dev/null 2>&1; then
		echo "[docker-e2e] /generate response failed validation" >&2
		echo "      expected: no error; primary.class==taekwon_kid;" >&2
		echo "                ≥1 snapshot with .score; ≥1 snapshot with gates" >&2
		exit 1
	fi
	GEN_ITERS=$(jq -r '.iters' "$TMP/generate-response.json")
	GEN_SNAP_COUNT=$(jq -r '.primary.snapshots | length' "$TMP/generate-response.json")
	GEN_SCORED_COUNT=$(jq -r '[.primary.snapshots[] | select(.score != null)] | length' "$TMP/generate-response.json")
	GEN_GATED_COUNT=$(jq -r '[.primary.snapshots[] | select(.gates != null and (.gates | length) > 0)] | length' "$TMP/generate-response.json")
	echo "[docker-e2e] /generate OK (iters=$GEN_ITERS snapshots=$GEN_SNAP_COUNT scored=$GEN_SCORED_COUNT gated=$GEN_GATED_COUNT)"
else
	# LLM_API_KEY unset: API starts in score-only mode; /generate must
	# return 503 with "LLM provider not configured" rather than crashing
	# or accepting a job that can't be processed. Verify rather than skip.
	echo "[docker-e2e] LLM_API_KEY not set; verifying /generate returns 503"
	HTTP_CODE=$(curl -s -o "$TMP/generate-503.json" -w '%{http_code}' \
		--max-time 10 \
		-X POST "$API_URL/generate" \
		-H 'content-type: application/json' \
		-d '{"class":"taekwon_kid","server":"uaro","playstyle":"pvm","description":"503 probe"}')
	if [[ "$HTTP_CODE" != "503" ]]; then
		echo "[docker-e2e] /generate returned HTTP $HTTP_CODE (expected 503 when LLM_API_KEY unset)" >&2
		cat "$TMP/generate-503.json" >&2 || true
		exit 1
	fi
	if ! jq -e '(.error // "") | contains("LLM provider not configured")' "$TMP/generate-503.json" >/dev/null 2>&1; then
		echo "[docker-e2e] /generate 503 body missing 'LLM provider not configured'" >&2
		cat "$TMP/generate-503.json" >&2 || true
		exit 1
	fi
	echo "[docker-e2e] /generate 503 confirmed (score-only mode)"
fi

echo "[docker-e2e] all checks passed"
