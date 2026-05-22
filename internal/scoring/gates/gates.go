// Package gates is the quality-gates evaluator the orchestrator runs on
// each canonically-scored snapshot. Reads the snapshot, the calc
// ScoreResponse, the catalog, and the active server profile + request
// overrides; emits a list of GateResults across five categories:
//
//  1. requirements_met ; stat-cap / valid skills / valid slots
//  2. offensive        ; HIT, FLEE-when-dodge-relied-on, primary-skill
//     rotation, per-skill cast time
//  3. survival_hp      ; incoming.max ratio + content-derived absolute
//     floor (incoming.ave × battleTimeSec)
//  4. status_immunity  ; target's threats × build's stat/equip coverage,
//     filtered by per-scenario priority list
//  5. uninterruptible_cast; Phen/Orleans for any long-cast skill
//
// Gates derive from build + skills + equipment + content. NO archetype
// labels. If a check needs an archetype label to evaluate, it's
// structured wrong.
package gates

import (
	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// Severity / Result aliases re-export the domain types so callers in
// this package can write `gates.SeverityFail` / `gates.Result` without
// importing domain directly. Single source of truth lives in
// internal/domain (so Snapshot.Gates can use the same type without an
// import cycle).
const (
	SeverityPass     = domain.GateSeverityPass
	SeverityWarn     = domain.GateSeverityWarn
	SeverityFail     = domain.GateSeverityFail
	SeverityFailHard = domain.GateSeverityFailHard
)

// Result is the per-gate emission shape. Aliased to domain.GateResult.
type Result = domain.GateResult

// Inputs is the bundle the evaluator needs to produce results for one
// snapshot. Score may be nil when canonical scoring failed for this
// snapshot; gates that depend on calc output skip themselves; gates
// that only need build data (requirements_met, status_immunity) still
// run.
//
// Catalog is required (not nil); gate logic looks up skill metadata
// (cast time / cooldown / interruptible) and item immunity tags from
// the embedded catalog.
type Inputs struct {
	Score     *scoring.ScoreResponse
	Snapshot  *domain.Snapshot
	Catalog   *catalog.Catalog
	Profile   *domain.ServerProfile // may be nil for profile-agnostic /score calls
	Scenario  *domain.Scenario      // the target this snapshot is being scored against
	Overrides *domain.GateOverrides // request-level gate relaxations; nil = "no overrides"

	// Playstyle is the request-level "pvm" / "pvp" / "balanced_*" hint
	// used as a fallback when Scenario doesn't carry an explicit content
	// type. WoE folds into pvp.
	Playstyle string
}

// Evaluate runs every applicable gate and returns the union of their
// results. Pure function; no I/O, no global state. Order of results is
// deterministic (gate-by-gate), which keeps `git diff` on test fixtures
// stable.
func Evaluate(in Inputs) []Result {
	var out []Result
	out = append(out, evaluateRequirements(in)...)
	out = append(out, evaluateTaekwonKickStances(in)...)
	out = append(out, evaluateHit(in)...)
	out = append(out, evaluateFlee(in)...)
	out = append(out, evaluateEHP(in)...)
	out = append(out, evaluateTTK(in)...)
	out = append(out, evaluateCastTime(in)...)
	out = append(out, evaluateRotation(in)...)
	out = append(out, evaluateStatusImmunity(in)...)
	out = append(out, evaluateUninterruptibleCast(in)...)
	return out
}

// HasFail reports whether any of the results is fail or fail_hard. Used
// by the orchestrator's save-to-library guard: hard fails reject the
// trajectory; warns flow through.
func HasFail(results []Result) bool {
	for _, r := range results {
		if r.Severity == SeverityFail || r.Severity == SeverityFailHard {
			return true
		}
	}
	return false
}
