package gates

import (
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/data"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// loadCatalog provides a real catalog for tests. The embedded asset is
// a build-time artifact, so this never errors in a healthy repo.
func loadCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	return c
}

// floatPtr / intPtr help build *float64 / *int fields the scoring
// types use to express "could not compute".
func floatPtr(v float64) *float64 { return &v }

// canonicalProfile is a minimal ServerProfile carrying just the
// QualityGates the tests need. Lets us avoid loading uaro.yaml in
// every test (and keeps tests independent of UARO-specific tuning).
func canonicalProfile() *domain.ServerProfile {
	qg := canonicalQualityGates()
	return &domain.ServerProfile{
		Key:          "test",
		QualityGates: &qg,
	}
}

// scoreResp is a small builder for ScoreResponse fixtures. Only the
// fields the gate under test reads need to be set; the rest stay at
// zero/nil.
//
// `dodge` populates CombatResults.Dodge; the per-encounter dodge
// percentage the FLEE gate consults. Argument name kept as `flee` for
// readability since the gate is named `flee_floor` and the parameter
// represents the same concept (pre-renewal dodge rate driven by FLEE).
func scoreResp(maxHp int, hit, flee float64, incomingMax, incomingAve, battleTime float64, enemyType string) *scoring.ScoreResponse {
	return &scoring.ScoreResponse{
		Derived: scoring.DerivedStats{MaxHP: maxHp},
		Combat: &scoring.CombatResults{
			// Default damage.ave is non-nil so gates treat this as "calc
			// modeled the build's offense" (the auto-attack-only TK case
			// is tested explicitly by setting Damage.Ave = nil).
			Damage:        scoring.CombatDamage{Ave: floatPtr(100.0)},
			Hit:           floatPtr(hit),
			Dodge:         floatPtr(flee),
			BattleTimeSec: floatPtr(battleTime),
			Incoming: scoring.CombatIncoming{
				Max:          floatPtr(incomingMax),
				Ave:          floatPtr(incomingAve),
				AveWithDodge: floatPtr(incomingAve), // overridden in dodge tests
			},
			Enemy: scoring.CombatEnemy{Type: enemyType},
		},
	}
}

// findResult is a small helper: returns the first result with the
// given name, or zero value + false. Tests use it to assert one
// specific gate's outcome without hardcoding ordering.
func findResult(rs []Result, name string) (Result, bool) {
	for _, r := range rs {
		if r.Name == name {
			return r, true
		}
	}
	return Result{}, false
}

// HIT 96% on a normal mob → warn (below NormalWarn=100% but above
// NormalFail=95%). Common case: the LLM should know but build can ship.
func TestEvaluateHit_NormalWarnBelow100(t *testing.T) {
	in := Inputs{
		Score:   scoreResp(10000, 96.0, 0, 0, 0, 0, "Normal"),
		Profile: canonicalProfile(),
	}
	res := evaluateHit(in)
	r, ok := findResult(res, "hit_floor_normal")
	if !ok {
		t.Fatalf("hit_floor_normal missing; got %+v", res)
	}
	if r.Severity != SeverityWarn {
		t.Errorf("severity: got %q want warn", r.Severity)
	}
}

// HIT 96% on an MVP target → pass. MVP gate is strict but only at 95%
// and below; 96% clears.
func TestEvaluateHit_MVP96Passes(t *testing.T) {
	in := Inputs{
		Score:   scoreResp(10000, 96.0, 0, 0, 0, 0, "MVP"),
		Profile: canonicalProfile(),
	}
	r, _ := findResult(evaluateHit(in), "hit_floor_mvp")
	if r.Severity != SeverityPass {
		t.Errorf("severity: got %q want pass", r.Severity)
	}
}

// HIT 70% on a normal mob → fail-hard (below 80%). Half your swings
// whiff; the build is broken for that target.
func TestEvaluateHit_BelowFloorIsFailHard(t *testing.T) {
	in := Inputs{
		Score:   scoreResp(10000, 70.0, 0, 0, 0, 0, "Normal"),
		Profile: canonicalProfile(),
	}
	r, _ := findResult(evaluateHit(in), "hit_floor_normal")
	if r.Severity != SeverityFailHard {
		t.Errorf("severity: got %q want fail_hard", r.Severity)
	}
}

// Taekwon classes' primary offense is kicks (counter kick is
// unconditional; the ranker build hinges on it). The calc reports the
// auto-attack hit-rate but it's not the right quality lens, so the
// gate demotes fail/fail_hard to warn; the data still surfaces to
// the LLM but doesn't block a viable trajectory.
func TestEvaluateHit_TaekwonClass_DemotesFailHardToWarn(t *testing.T) {
	resp := scoreResp(10000, 54.0, 0, 0, 0, 0, "Normal")
	in := Inputs{
		Score:    resp,
		Profile:  canonicalProfile(),
		Snapshot: &domain.Snapshot{Class: "taekwon_kid"},
	}
	r, ok := findResult(evaluateHit(in), "hit_floor_normal")
	if !ok {
		t.Fatalf("hit_floor_normal missing; got %+v", evaluateHit(in))
	}
	if r.Severity != SeverityWarn {
		t.Errorf("severity: got %q want warn (demoted from fail_hard)", r.Severity)
	}
	if !strings.Contains(r.Reason, "auto-attack hit-rate isn't the right quality lens") {
		t.Errorf("reason should explain the demotion; got %q", r.Reason)
	}
}

// Magic-primary build (MATK exceeds total ATK) demotes regardless of
// class. Models a hybrid build pivoted to spells via gear.
func TestEvaluateHit_MatkDominant_DemotesFailHardToWarn(t *testing.T) {
	resp := scoreResp(10000, 70.0, 0, 0, 0, 0, "Normal")
	resp.Derived.Atk = scoring.BasePlus{Base: 80, Plus: 20} // total atk 100
	resp.Derived.Matk = scoring.MinMax{Min: 300, Max: 400}  // matk dominates
	in := Inputs{
		Score:    resp,
		Profile:  canonicalProfile(),
		Snapshot: &domain.Snapshot{Class: "knight"}, // melee class, but magic-built
	}
	r, _ := findResult(evaluateHit(in), "hit_floor_normal")
	if r.Severity != SeverityWarn {
		t.Errorf("severity: got %q want warn (demoted from fail_hard)", r.Severity)
	}
}

// Same Taekwon class but the gate would normally pass; demotion only
// applies to fail/fail_hard, never bumps pass to anything else.
func TestEvaluateHit_TaekwonClass_PassStaysPass(t *testing.T) {
	resp := scoreResp(10000, 100.0, 0, 0, 0, 0, "Normal")
	in := Inputs{
		Score:    resp,
		Profile:  canonicalProfile(),
		Snapshot: &domain.Snapshot{Class: "taekwon_kid"},
	}
	r, _ := findResult(evaluateHit(in), "hit_floor_normal")
	if r.Severity != SeverityPass {
		t.Errorf("severity: got %q want pass (no demotion needed)", r.Severity)
	}
}

// A melee class with no magic gear keeps fail_hard; the demotion
// shouldn't fire for builds where HIT genuinely matters.
func TestEvaluateHit_MeleeClass_KeepsFailHard(t *testing.T) {
	resp := scoreResp(10000, 54.0, 0, 0, 0, 0, "Normal")
	resp.Derived.Atk = scoring.BasePlus{Base: 400, Plus: 100}
	resp.Derived.Matk = scoring.MinMax{Min: 50, Max: 60}
	in := Inputs{
		Score:    resp,
		Profile:  canonicalProfile(),
		Snapshot: &domain.Snapshot{Class: "knight"},
	}
	r, _ := findResult(evaluateHit(in), "hit_floor_normal")
	if r.Severity != SeverityFailHard {
		t.Errorf("severity: got %q want fail_hard (no demotion)", r.Severity)
	}
}

// FLEE gate skips when the build isn't dodge-driven (aveWithDodge
// near ave). A tank build with 5 FLEE shouldn't fail on FLEE.
func TestEvaluateFlee_TankBuildSkipsGate(t *testing.T) {
	resp := scoreResp(20000, 0, 5.0, 0, 1000, 0, "Normal")
	resp.Combat.Incoming.AveWithDodge = floatPtr(1000) // dodge does no work
	in := Inputs{Score: resp, Profile: canonicalProfile()}
	if got := evaluateFlee(in); got != nil {
		t.Errorf("expected gate to skip; got %+v", got)
	}
}

// FLEE gate fires for a dodge-tanker at 85%; below 90% fail floor.
func TestEvaluateFlee_DodgeTankerBelowFloorFails(t *testing.T) {
	resp := scoreResp(8000, 0, 85.0, 0, 1000, 0, "Normal")
	resp.Combat.Incoming.AveWithDodge = floatPtr(150) // dodge cuts incoming by 85%
	in := Inputs{Score: resp, Profile: canonicalProfile()}
	r, _ := findResult(evaluateFlee(in), "flee_floor")
	if r.Severity != SeverityFail {
		t.Errorf("severity: got %q want fail", r.Severity)
	}
}

// EHP layer A: maxHp 8k vs incoming.max 12k MVP melee → one-shot fail-hard.
func TestEvaluateEHP_OneShotIsFailHard(t *testing.T) {
	in := Inputs{
		Score:   scoreResp(8000, 0, 0, 12000, 0, 0, "MVP"),
		Profile: canonicalProfile(),
	}
	res := evaluateEHP(in)
	if len(res) == 0 {
		t.Fatal("expected at least one EHP result")
	}
	if res[0].Severity != SeverityFailHard {
		t.Errorf("severity: got %q want fail_hard", res[0].Severity)
	}
}

// EHP layer A: 17k LK BB vs MVP melee 4k big-hit → 4.25× ratio, well
// above 2.0× MVP melee floor. Validates Lacielle's empirical breakpoint
// from ref_docs lands as a pass.
func TestEvaluateEHP_LacielleLKBBClearsMVPMelee(t *testing.T) {
	in := Inputs{
		Score:   scoreResp(17000, 0, 0, 4000, 0, 0, "MVP"),
		Profile: canonicalProfile(),
	}
	res := evaluateEHP(in)
	r, ok := findResult(res, "ehp_burst_ratio_mvp_melee")
	if !ok {
		t.Fatalf("ehp_burst_ratio_mvp_melee missing; got %+v", res)
	}
	if r.Severity != SeverityPass {
		t.Errorf("severity: got %q want pass", r.Severity)
	}
}

// Status immunity: SinX with 100 VIT covers stun via stat. Amdarais
// applies stun + curse; in PvM, curse is warn (LK build doesn't have
// curse coverage); stun fires as pass.
func TestEvaluateStatusImmunity_StatCoverage(t *testing.T) {
	cat := loadCatalog(t)
	in := Inputs{
		Snapshot: &domain.Snapshot{
			Stats: domain.Stats{Vit: 100},
		},
		Catalog:  cat,
		Profile:  canonicalProfile(),
		Scenario: &domain.Scenario{Target: 1373}, // Lord of Death; applies [stun, curse]
	}
	res := evaluateStatusImmunity(in)
	stunR, _ := findResult(res, "status_immunity_stun")
	if stunR.Severity != SeverityPass {
		t.Errorf("stun coverage via 100 VIT: got %q want pass", stunR.Severity)
	}
	curseR, _ := findResult(res, "status_immunity_curse")
	if curseR.Severity != SeverityWarn {
		t.Errorf("curse uncovered in PvM: got %q want warn (curse is in pvm.warn list)", curseR.Severity)
	}
}

// Request override: user says "ignore stun" → status_immunity_stun
// gate doesn't fire even though target applies stun and build doesn't
// cover it.
func TestEvaluateStatusImmunity_RequestOverrideDropsStatus(t *testing.T) {
	cat := loadCatalog(t)
	in := Inputs{
		Snapshot: &domain.Snapshot{Stats: domain.Stats{Vit: 1}},
		Catalog:  cat,
		Profile:  canonicalProfile(),
		Scenario: &domain.Scenario{Target: 1115}, // Eddga; applies [stun]
		Overrides: &domain.GateOverrides{
			IgnoreStatus: []string{"stun"},
		},
	}
	res := evaluateStatusImmunity(in)
	if _, ok := findResult(res, "status_immunity_stun"); ok {
		t.Errorf("stun gate should be dropped via override; got %+v", res)
	}
}

func TestIUC_PhenPasses(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 99)
	// Phen card (4077) in an accessory grants uninterruptible cast.
	in.Snapshot.Equipment = map[domain.SlotKey]domain.EquipSpec{
		"accessory1": {ID: 2785, Cards: []int{4077}},
	}
	r, ok := findResult(evaluateUninterruptibleCast(in), "uninterruptible_cast")
	if !ok || r.Severity != SeverityPass {
		t.Fatalf("want pass with Phen; got %+v ok=%v", r, ok)
	}
}

func TestIUC_NoMitigationFails(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 99) // 2380ms, interruptible, no gear
	r, ok := findResult(evaluateUninterruptibleCast(in), "uninterruptible_cast")
	if !ok || r.Severity != SeverityFail {
		t.Fatalf("want fail with no cast-safety; got %+v ok=%v", r, ok)
	}
}

func TestIUC_SafetyWallDemotesToWarn(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 99)
	in.Snapshot.Skills = append(in.Snapshot.Skills, scoring.SkillAlloc{ID: 12, Level: 1}) // Safety Wall
	r, ok := findResult(evaluateUninterruptibleCast(in), "uninterruptible_cast")
	if !ok {
		t.Fatal("no result returned")
	}
	if r.Severity != SeverityWarn {
		t.Fatalf("Safety Wall should demote to warn; got %+v", r)
	}
}

func TestIUC_HighDodgeDemotesToWarn(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 99)
	dodge := 85.0
	in.Score.Combat = &scoring.CombatResults{Dodge: &dodge}
	r, ok := findResult(evaluateUninterruptibleCast(in), "uninterruptible_cast")
	if !ok {
		t.Fatal("no result returned")
	}
	if r.Severity != SeverityWarn {
		t.Fatalf("high dodge should demote to warn; got %+v", r)
	}
}

func TestIUC_FastCastNoFire(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 145) // ~233ms effective < 2000
	if got := evaluateUninterruptibleCast(in); got != nil {
		t.Fatalf("fast cast should not fire; got %+v", got)
	}
}

func TestIUC_IceWallDemotesToWarn(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 99)
	in.Snapshot.Skills = append(in.Snapshot.Skills, scoring.SkillAlloc{ID: 87, Level: 1}) // Ice Wall
	r, ok := findResult(evaluateUninterruptibleCast(in), "uninterruptible_cast")
	if !ok {
		t.Fatal("no result returned")
	}
	if r.Severity != SeverityWarn {
		t.Fatalf("Ice Wall should demote to warn; got %+v", r)
	}
}

func TestIUC_FireWallDemotesToWarn(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 99)
	in.Snapshot.Skills = append(in.Snapshot.Skills, scoring.SkillAlloc{ID: 18, Level: 1}) // Fire Wall
	r, ok := findResult(evaluateUninterruptibleCast(in), "uninterruptible_cast")
	if !ok {
		t.Fatal("no result returned")
	}
	if r.Severity != SeverityWarn {
		t.Fatalf("Fire Wall should demote to warn; got %+v", r)
	}
}

func TestIUC_NonInterruptiblePrimaryNoFire(t *testing.T) {
	// Pressure: 4000ms cast (3200ms effective at 30 DEX, well over the threshold)
	// but non-interruptible, so the interruption gate must not fire.
	in := castTestInputs(t, "pressure", 367, 5, 30)
	if got := evaluateUninterruptibleCast(in); got != nil {
		t.Fatalf("non-interruptible primary should not fire; got %+v", got)
	}
}

func TestIUC_NoPrimaryNoFire(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 99)
	in.Snapshot.ScoredSkills = nil // no primary designated -> nothing to gate
	if got := evaluateUninterruptibleCast(in); got != nil {
		t.Fatalf("no primary should not fire; got %+v", got)
	}
}

// castTestInputs builds an Inputs for the casting gates: the real embedded
// catalog, a snapshot whose primary scored skill is primaryName (allocated as
// skillID at the given level), and a calc score reporting effective DEX. Combat
// is non-nil so tests can set dodge.
func castTestInputs(t *testing.T, primaryName string, skillID, level, dex int) Inputs {
	t.Helper()
	return Inputs{
		Catalog: loadCatalog(t),
		Snapshot: &domain.Snapshot{
			Class:        "high_wizard",
			Stats:        domain.Stats{Dex: dex},
			Skills:       []scoring.SkillAlloc{{ID: skillID, Level: level}},
			ScoredSkills: []domain.ScoredSkill{{Name: primaryName, Primary: true}},
		},
		Score: &scoring.ScoreResponse{
			Derived: scoring.DerivedStats{TotalStats: domain.Stats{Dex: dex}},
			Combat:  &scoring.CombatResults{},
		},
		Profile: nil,
	}
}

func TestEvaluateCastTime_SlowPrimaryWarns(t *testing.T) {
	in := castTestInputs(t,
		/*primary*/ "storm_gust", /*skillID*/ 89, /*level*/ 10, /*dex*/ 99)
	r, ok := findResult(evaluateCastTime(in), "cast_time_WZ_STORMGUST")
	if !ok {
		t.Fatalf("expected slowness warn; got %+v", evaluateCastTime(in))
	}
	if r.Severity != SeverityWarn {
		t.Errorf("severity = %v, want warn", r.Severity)
	}
}

func TestEvaluateCastTime_GearedBoltClears(t *testing.T) {
	// Cold Bolt Lv10 base 7000ms; at 108 effective DEX -> 1960ms < 3000 warn.
	in := castTestInputs(t, "cold_bolt", 14, 10, 108)
	if got := evaluateCastTime(in); got != nil {
		t.Errorf("expected no warn at 108 DEX, got %+v", got)
	}
}

func TestEvaluateCastTime_NonPrimaryIgnored(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 30) // 30 DEX -> Cold Bolt 5600ms fires (> 3000 warn)
	// Lightning Bolt (id 20) is allocated but NOT primary; it must not be gated.
	in.Snapshot.Skills = append(in.Snapshot.Skills, scoring.SkillAlloc{ID: 20, Level: 10})
	results := evaluateCastTime(in)
	if _, ok := findResult(results, "cast_time_MG_COLDBOLT"); !ok {
		t.Fatalf("expected primary Cold Bolt to warn at 30 DEX; got %+v", results)
	}
	if _, ok := findResult(results, "cast_time_MG_LIGHTNINGBOLT"); ok {
		t.Fatal("non-primary Lightning Bolt should not be gated")
	}
}

// effectiveCastTimeMs unit cases: basic formula sanity.
func TestEffectiveCastTimeMs(t *testing.T) {
	cases := []struct {
		name      string
		base, fix int
		dex       int
		want      int
	}{
		{"15s @ 0 DEX", 15000, 0, 0, 15000},
		{"15s @ 75 DEX = 7500ms", 15000, 0, 75, 7500},
		{"15s @ 150 DEX = 0ms (IC)", 15000, 0, 150, 0},
		{"15s @ 99 DEX (Bragi cap)", 15000, 0, 99, 15000 * 51 / 150}, // ~5100
		{"with fixed portion", 5000, 1000, 150, 1000},                // fixed always remains
		{"negative DEX clamps to 0", 5000, 0, -10, 5000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := effectiveCastTimeMs(c.base, c.fix, c.dex)
			if got != c.want {
				t.Errorf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestBuildCovers_StunUsesEffectiveVit(t *testing.T) {
	// Base allocation 97 VIT, +3 from gear -> effective 100 -> stun-immune.
	in := Inputs{
		Snapshot: &domain.Snapshot{Stats: domain.Stats{Vit: 97}},
		Score: &scoring.ScoreResponse{
			Derived: scoring.DerivedStats{TotalStats: domain.Stats{Vit: 100}},
		},
	}
	if !buildCovers(in, data.ImmunityStun) {
		t.Fatal("effective VIT 100 should grant stun immunity")
	}
}

// Regression: a fully-geared endgame High Wizard (Cold Bolt primary, effective
// DEX 108, Phen accessory, Fire/Lightning Bolt + Jupitel allocated but not
// primary) must produce zero cast warnings or failures. The incident produced
// four bogus cast warnings: one for Cold Bolt (incorrectly slow at base DEX)
// and one each for the three non-primary bolts being gated when they should
// have been ignored.
func TestRegression_GearedHWNoSpuriousCastWarnings(t *testing.T) {
	in := castTestInputs(t, "cold_bolt", 14, 10, 108) // effective DEX 108
	in.Snapshot.Skills = append(in.Snapshot.Skills,
		scoring.SkillAlloc{ID: 19, Level: 10}, // Fire Bolt
		scoring.SkillAlloc{ID: 20, Level: 10}, // Lightning Bolt
		scoring.SkillAlloc{ID: 84, Level: 10}, // Jupitel
	)
	in.Snapshot.Equipment = map[domain.SlotKey]domain.EquipSpec{
		"accessory1": {ID: 2785, Cards: []int{4077}}, // Phen
	}
	results := append(evaluateCastTime(in), evaluateUninterruptibleCast(in)...)
	for _, r := range results {
		if r.Severity == SeverityWarn || r.Severity == SeverityFail || r.Severity == SeverityFailHard {
			t.Errorf("unexpected non-pass cast result: %+v", r)
		}
	}
}
