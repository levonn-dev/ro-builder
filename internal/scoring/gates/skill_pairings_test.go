package gates

import (
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// Skill IDs from Hercules pre-re skill_db.conf; kept inline so test
// breakage is obvious if catalog ids ever drift from emulator data.
const (
	skillTKStormkick    = 413
	skillTKReadystorm   = 412
	skillTKDownkick     = 415
	skillTKReadydown    = 414
	skillTKTurnkick     = 417
	skillTKReadyturn    = 416
	skillTKCounter      = 419
	skillTKReadycounter = 418
	skillTKJumpkick     = 421
)

// Kick allocated without its stance → fail gate fires with both skill
// names in the reason.
func TestKickStance_StormkickWithoutStance_Fails(t *testing.T) {
	cat := loadCatalog(t)
	in := Inputs{
		Snapshot: &domain.Snapshot{
			Class:  "taekwon_kid",
			Skills: []domain.SkillAlloc{{ID: skillTKStormkick, Level: 7}},
		},
		Catalog: cat,
	}
	r, ok := findResult(evaluateTaekwonKickStances(in), "taekwon_kick_missing_stance")
	if !ok {
		t.Fatalf("gate missing; got %+v", evaluateTaekwonKickStances(in))
	}
	if r.Severity != SeverityFail {
		t.Errorf("severity: got %q want fail", r.Severity)
	}
	if !strings.Contains(r.Reason, "TK_STORMKICK") || !strings.Contains(r.Reason, "TK_READYSTORM") {
		t.Errorf("reason should name both skills; got %q", r.Reason)
	}
}

// Kick + stance both allocated → no gate fires.
func TestKickStance_StormkickWithStance_Passes(t *testing.T) {
	cat := loadCatalog(t)
	in := Inputs{
		Snapshot: &domain.Snapshot{
			Class: "taekwon_kid",
			Skills: []domain.SkillAlloc{
				{ID: skillTKStormkick, Level: 7},
				{ID: skillTKReadystorm, Level: 1},
			},
		},
		Catalog: cat,
	}
	if res := evaluateTaekwonKickStances(in); len(res) != 0 {
		t.Errorf("expected no gate results, got %+v", res)
	}
}

// Flying Kick has no stance; allocating it alone must not fire.
func TestKickStance_JumpkickAloneIsFine(t *testing.T) {
	cat := loadCatalog(t)
	in := Inputs{
		Snapshot: &domain.Snapshot{
			Class:  "taekwon_kid",
			Skills: []domain.SkillAlloc{{ID: skillTKJumpkick, Level: 7}},
		},
		Catalog: cat,
	}
	if res := evaluateTaekwonKickStances(in); len(res) != 0 {
		t.Errorf("Flying Kick has no stance; expected no gate results, got %+v", res)
	}
}

// Multiple kicks each missing their stance → single gate result names
// every offender, sorted for deterministic output.
func TestKickStance_MultipleKicksMissingStances(t *testing.T) {
	cat := loadCatalog(t)
	in := Inputs{
		Snapshot: &domain.Snapshot{
			Class: "taekwon_kid",
			Skills: []domain.SkillAlloc{
				{ID: skillTKStormkick, Level: 7},
				{ID: skillTKDownkick, Level: 7},
				{ID: skillTKCounter, Level: 7},
			},
		},
		Catalog: cat,
	}
	r, _ := findResult(evaluateTaekwonKickStances(in), "taekwon_kick_missing_stance")
	for _, name := range []string{"TK_STORMKICK", "TK_DOWNKICK", "TK_COUNTER"} {
		if !strings.Contains(r.Reason, name) {
			t.Errorf("reason missing %q; got %q", name, r.Reason)
		}
	}
}

// Level 0 in the skill list means "not allocated"; shouldn't trip the
// gate. Calc-shim contract: lv 0 entries are tolerated as no-ops.
func TestKickStance_ZeroLevelIsNotAllocated(t *testing.T) {
	cat := loadCatalog(t)
	in := Inputs{
		Snapshot: &domain.Snapshot{
			Class:  "taekwon_kid",
			Skills: []domain.SkillAlloc{{ID: skillTKStormkick, Level: 0}},
		},
		Catalog: cat,
	}
	if res := evaluateTaekwonKickStances(in); len(res) != 0 {
		t.Errorf("lv 0 should be ignored, got %+v", res)
	}
}
