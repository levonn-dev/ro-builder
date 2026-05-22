#!/usr/bin/env bash
# generate.sh; run one /generate request (async), poll until complete,
# fetch the enriched /builds/{id} view, and render the resulting endgame
# build inline.
#
# Usage:
#   scripts/generate.sh                            # default: TK PvM ranker
#   scripts/generate.sh path/to/body.json          # custom request body
#
# Outputs land under tmp/ (gitignored):
#   tmp/enqueue.json                     ; 202 enqueue response (contains id)
#   tmp/build-<id>.json                  ; GET /builds/{id} enriched view
#
# The POST /generate now returns 202 + {id} immediately. This script polls
# GET /generations/{id} until status is completed or failed, then fetches
# the enriched build from GET /builds/{id}.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [[ -f .env ]]; then
	set -o allexport
	# shellcheck disable=SC1091
	source .env
	set +o allexport
fi

if ! command -v jq >/dev/null 2>&1; then
	echo "[generate] jq required" >&2
	exit 2
fi

if [[ -z "${LLM_API_KEY:-}" ]]; then
	echo "[generate] LLM_API_KEY not set; /generate is disabled" >&2
	exit 2
fi

if [[ $# -ge 1 ]]; then
	if [[ ! -f "$1" ]]; then
		echo "[generate] body file not found: $1" >&2
		exit 2
	fi
	REQUEST_BODY="$(cat "$1")"
	REQUEST_LABEL="$1"
else
	REQUEST_BODY='{
		"class": "taekwon_kid",
		"server": "uaro",
		"playstyle": "pvm",
		"description": "General PvM Taekwon Ranker"
	}'
	REQUEST_LABEL='(default TK PvM ranker)'
fi

mkdir -p tmp

TMP="$(mktemp -d -t ro-builder-gen.XXXXXX)"

cleanup() {
	local code=$?
	if [[ -n "${API_PID:-}" ]]; then kill "$API_PID" 2>/dev/null || true; fi
	if [[ -n "${SIDECAR_PID:-}" ]]; then kill "$SIDECAR_PID" 2>/dev/null || true; fi
	wait 2>/dev/null || true
	if (( code != 0 )) && [[ -f "$TMP/api.log" ]]; then
		echo
		echo "[generate] FAILED (exit $code); api.log tail:"
		tail -40 "$TMP/api.log"
	fi
	rm -rf "$TMP"
}
trap cleanup EXIT

# Pick random ports per run so concurrent invocations (or a stale instance
# still tearing down) don't collide on a fixed pair. Range stays well above
# the dev defaults (7401/8080) and e2e.sh's reserved ports (17401/18080).
GEN_SIDECAR_PORT="$(shuf -i 20000-29999 -n 1)"
GEN_API_PORT="$(shuf -i 30000-39999 -n 1)"
SIDECAR_URL="http://localhost:$GEN_SIDECAR_PORT"
API_URL="http://localhost:$GEN_API_PORT"
GEN_DB="$TMP/buildlibrary.db"

echo "[generate] request: $REQUEST_LABEL"

# --- sidecar ---
echo "[generate] starting sidecar on :$GEN_SIDECAR_PORT"
(cd calc-sidecar && PORT=$GEN_SIDECAR_PORT exec node server.ts) >"$TMP/sidecar.log" 2>&1 &
SIDECAR_PID=$!
for _ in $(seq 1 60); do
	if ! kill -0 "$SIDECAR_PID" 2>/dev/null; then
		echo "[generate] sidecar process exited before becoming ready; check sidecar.log" >&2
		exit 1
	fi
	curl -sf -o /dev/null -X POST "$SIDECAR_URL/score" \
		-H 'content-type: application/json' -d '{}' 2>/dev/null && break
	sleep 0.5
done
curl -sf -o /dev/null -X POST "$SIDECAR_URL/score" \
	-H 'content-type: application/json' -d '{}' \
	|| { echo "[generate] sidecar did not become ready" >&2; exit 1; }

# --- api (build, then exec; avoids the go-run-leaked-child trap) ---
echo "[generate] building + starting api on :$GEN_API_PORT"
go build -o "$TMP/api" ./cmd/api >"$TMP/api-build.log" 2>&1 \
	|| { echo "[generate] api build failed"; tail -20 "$TMP/api-build.log" >&2; exit 1; }
(
	ADDR=":$GEN_API_PORT" SIDECAR_URL="$SIDECAR_URL" BUILDLIBRARY_PATH="$GEN_DB" \
		exec "$TMP/api"
) >"$TMP/api.log" 2>&1 &
API_PID=$!
for _ in $(seq 1 120); do
	if ! kill -0 "$API_PID" 2>/dev/null; then
		echo "[generate] api process exited before becoming ready; check api.log" >&2
		exit 1
	fi
	curl -sf -o /dev/null -X POST "$API_URL/score" \
		-H 'content-type: application/json' -d '{}' 2>/dev/null && break
	sleep 0.5
done
curl -sf -o /dev/null -X POST "$API_URL/score" \
	-H 'content-type: application/json' -d '{}' \
	|| { echo "[generate] api did not become ready" >&2; exit 1; }

# --- /generate (async) ---
echo "[generate] POST /generate"
HTTP_CODE=$(curl -s -o "$TMP/enqueue.json" -w '%{http_code}' \
	--max-time 30 \
	-X POST "$API_URL/generate" -H 'content-type: application/json' \
	-d "$REQUEST_BODY")
if [[ "$HTTP_CODE" != "202" ]]; then
	echo "[generate] enqueue HTTP $HTTP_CODE; response:" >&2
	jq . "$TMP/enqueue.json" >&2 2>/dev/null || cat "$TMP/enqueue.json" >&2
	exit 1
fi
GEN_ID="$(jq -r '.id' "$TMP/enqueue.json")"
echo "[generate] enqueued: $GEN_ID"

# --- poll /generations/{id} until terminal ---
POLL_INTERVAL=10  # seconds; short for the script; production clients should use ~30s
MAX_WAIT_SECONDS=900  # 15-minute cap on the script-side wait
START=$(date +%s)
LAST_STATUS=""
while :; do
	if ! curl -sf -o "$TMP/status.json" "$API_URL/generations/$GEN_ID"; then
		echo "[generate] poll failed" >&2
		exit 1
	fi
	STATUS="$(jq -r '.status' "$TMP/status.json")"
	if [[ "$STATUS" != "$LAST_STATUS" ]]; then
		echo "[generate] status: $STATUS"
		LAST_STATUS="$STATUS"
	fi
	case "$STATUS" in
		completed)
			break
			;;
		failed)
			ERR="$(jq -r '.error // "(no error)"' "$TMP/status.json")"
			echo "[generate] generation failed: $ERR" >&2
			echo "[generate] raw status: $TMP/status.json" >&2
			exit 1
			;;
		queued|running)
			NOW=$(date +%s)
			ELAPSED=$((NOW - START))
			if (( ELAPSED >= MAX_WAIT_SECONDS )); then
				echo "[generate] timed out after ${MAX_WAIT_SECONDS}s" >&2
				exit 1
			fi
			sleep "$POLL_INTERVAL"
			;;
		*)
			echo "[generate] unexpected status: $STATUS" >&2
			exit 1
			;;
	esac
done

BUILD_ID="$GEN_ID"
BUILD_RESP="$REPO_ROOT/tmp/build-$BUILD_ID.json"
echo "[generate] fetching enriched build"
if ! curl -sf -o "$BUILD_RESP" "$API_URL/builds/$BUILD_ID"; then
	echo "[generate] GET /builds/$BUILD_ID failed" >&2
	exit 1
fi

# Downstream rendering reads from $BUILD_RESP. The legacy GEN_RESP path
# (the raw /generate response) is no longer relevant; the async POST
# only returns an id, not the trajectory.
GEN_RESP="$BUILD_RESP"

echo

# --- render ---
#
# Render the endgame build in rocalc-style sections: header / stats /
# equipment+cards / skills / derived / gates. Item, card, skill, and
# mob names ride inline under each id reference in /builds/{id}'s
# response, so the pipeline reads .item.name / .name / .target.name
# directly with no lookup table.

jq -r '
	def fmt(x): if x == null then "-" elif (x | type) == "number" then (x | tostring) else "?" end;
	def name_or_unknown(ref):
		if ref == null then "-"
		elif (ref.name // "") == "" then "#\(ref.id) (unknown)"
		else ref.name
		end;
	def cards_inline(cards):
		if (cards // []) | length == 0 then ""
		else " · cards: " + ([cards[] | name_or_unknown(.)] | join(", "))
		end;

	(.primary.snapshots | last) as $hero |
	($hero.score // {}) as $sc |
	($sc.derived // {}) as $d |

	"═══ Endgame Build ═══",
	"Class:  \($hero.class)",
	"Level:  \($hero.level.base) / \($hero.level.job)" +
		(if $hero.post_rebirth then "  (post-rebirth)" else "" end) +
		(if $hero.leveling_target.target then "    leveling target: \(name_or_unknown($hero.leveling_target.target))" else "" end),
	"",
	"── Stats ──",
	"  STR \(($hero.stats.str // 0) | tostring | (" " * (4 - length)) + .)",
	"  AGI \(($hero.stats.agi // 0) | tostring | (" " * (4 - length)) + .)",
	"  VIT \(($hero.stats.vit // 0) | tostring | (" " * (4 - length)) + .)",
	"  INT \(($hero.stats.int // 0) | tostring | (" " * (4 - length)) + .)",
	"  DEX \(($hero.stats.dex // 0) | tostring | (" " * (4 - length)) + .)",
	"  LUK \(($hero.stats.luk // 0) | tostring | (" " * (4 - length)) + .)",
	"",
	"── Equipment & Cards ──",
	(if ($hero.equipment // {} | length) == 0 then "  (none)"
	 else
		($hero.equipment | to_entries | sort_by(.key)[] |
			"  " + (.key + ":") +
			"  " + name_or_unknown(.value.item) +
			"  +\(.value.refine // 0)" +
			cards_inline(.value.cards)
		)
	 end),
	"",
	"── Skills (\($hero.skills // [] | length) allocated) ──",
	(if ($hero.skills // []) | length == 0 then "  (none)"
	 else
		($hero.skills | sort_by(.id)[] |
			"  " + (if (.name // "") == "" then "#\(.id) (unknown)" else .name end) +
			"  lv \(.level)"
		)
	 end),
	"",
	"── Derived Stats ──",
	(if $d == {} then "  (snapshot not scored; calc skipped)"
	 else
		"  HIT  \(fmt($d.hit))   FLEE \(fmt($d.flee))   CRI \(fmt($d.cri))   ASPD \(fmt($d.aspd))",
		"  ATK  \(fmt($d.atk.base)) + \(fmt($d.atk.plus))   MATK \(fmt($d.matk.min))–\(fmt($d.matk.max))",
		"  DEF  \(fmt($d.def.hard)) / \(fmt($d.def.soft)) (hard/soft)   MDEF \(fmt($d.mdef.hard)) / \(fmt($d.mdef.soft))",
		"  MaxHP \(fmt($d.maxHp))   MaxSP \(fmt($d.maxSp))   Stat points left \(fmt($d.statPointsRemaining))"
	 end),
	"",
	(($sc.combat // null) as $cm |
	 if $cm == null then empty
	 else
		"── Vs Target (\($cm.enemy.race // "?") / \($cm.enemy.element // "?") / \($cm.enemy.size // "?"), HP \(fmt($cm.enemy.hp))) ──",
		"  your damage:  min/avg/max  \(fmt($cm.damage.min)) / \(fmt($cm.damage.ave)) / \(fmt($cm.damage.max))",
		"  incoming:     min/avg/max  \(fmt($cm.incoming.min)) / \(fmt($cm.incoming.ave)) / \(fmt($cm.incoming.max))",
		"  hit/flee:     \(fmt($cm.hit)) / \(fmt($cm.flee))    battle time \(fmt($cm.battleTimeSec))s",
		""
	 end),
	(
		(([$hero.gates // [] | .[] | select(.severity == "pass")] | length) as $p |
		 ([$hero.gates // [] | .[] | select(.severity == "warn")] | length) as $w |
		 ([$hero.gates // [] | .[] | select(.severity == "fail")] | length) as $f |
		 ([$hero.gates // [] | .[] | select(.severity == "fail_hard")] | length) as $fh |
		 "── Gates ──",
		 "  pass=\($p)  warn=\($w)  fail=\($f)  fail_hard=\($fh)   ⇒ GOOD" +
			(if $w > 0 then " (with \($w) warning\(if $w == 1 then "" else "s" end))" else "" end),
		 ($hero.gates // [] | .[] | select(.severity != "pass") |
			"    [\(.severity)] \(.name): \(.reason // "(no reason)")"
		 )
		)
	),
	"",
	"saved build id: \(.id)"
' "$BUILD_RESP"

echo
echo "[generate] full enriched JSON: $BUILD_RESP"
echo "[generate] browse history: curl $API_URL/builds | jq '.builds'"
