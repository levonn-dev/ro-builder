package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_AttackSkillDamage_RaisesScoredOffense proves the roundhouse
// kick attack-skill path flows through the full stack (resolver, contract,
// sidecar attack_skills driver, calc) and drives top-level damage above
// auto-attack for a Taekwon Kid. Boots its own calc-sidecar. Skipped under
// -short.
//
// Both builds are barehanded TK at 99/70 with identical stats; the base build
// declares no ScoredSkills (auto-attack), the scored build declares roundhouse
// as primary. The increase is the kick damage over bare-fist auto-attack.
// Target is a soft Level-20 neutral mob so hit rate is solvable.
func TestIntegration_AttackSkillDamage_RaisesScoredOffense(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: needs running sidecar")
	}
	baseURL, stop := startSidecar(t)
	defer stop()

	client := scoring.NewClient(baseURL, nil)
	cat := loadCat(t)

	// Soft neutral target: low level, zero defenses so hit rate is always
	// solvable and damage.ave is non-nil. Raise Dex or lower Level further
	// if this still yields nil.
	enemy := &scoring.EnemyStats{
		Hp: 30000, AtkMin: 50, AtkMax: 100, Def: 0, MDef: 0,
		Race: "RC_Brute", Element: "Ele_Neutral", ElementLv: 1, Size: "Size_Medium", Level: 20,
	}

	// Skills: TK_TURNKICK (roundhouse, id 417), the attack skill we score.
	// resolveAttackSkills only requires roundhouse allocated; this path calls
	// client.Score directly and runs no quality gates.
	skills := []domain.SkillAlloc{
		{ID: 417, Level: 7}, // TK_TURNKICK (roundhouse)
	}

	// Base: no ScoredSkills -> auto-attack damage.
	base := &domain.Snapshot{
		Class:  "taekwon_kid",
		Level:  domain.Level{Base: 99, Job: 70},
		Stats:  domain.Stats{Str: 90, Agi: 80, Vit: 40, Int: 1, Dex: 70, Luk: 1},
		Skills: skills,
	}
	reqBase := base.ToScoreRequest(nil)
	reqBase.EnemyInline = enemy
	respBase, err := client.Score(context.Background(), reqBase)
	if err != nil {
		t.Fatal(err)
	}

	// Scored: roundhouse as primary attack skill.
	scored := &domain.Snapshot{
		Class:        "taekwon_kid",
		Level:        domain.Level{Base: 99, Job: 70},
		Stats:        domain.Stats{Str: 90, Agi: 80, Vit: 40, Int: 1, Dex: 70, Luk: 1},
		Skills:       skills,
		ScoredSkills: []domain.ScoredSkill{{Name: "roundhouse", Primary: true}},
	}
	reqScored := scored.ToScoreRequest(nil)
	reqScored.EnemyInline = enemy
	resolved, err := resolveAttackSkills("taekwon_kid", scored.Skills, scored.ScoredSkills, cat)
	if err != nil {
		t.Fatal(err)
	}
	reqScored.AttackSkills = resolved
	respScored, err := client.Score(context.Background(), reqScored)
	if err != nil {
		t.Fatal(err)
	}

	if respBase.Combat == nil || respScored.Combat == nil {
		t.Fatal("combat results missing (enemy not applied?)")
	}
	baseAve := respBase.Combat.Damage.Ave
	scoredAve := respScored.Combat.Damage.Ave
	if baseAve == nil || scoredAve == nil {
		t.Fatalf("combat damage.ave is nil (unsolvable hit rate); raise dex/lower target def. base=%v scored=%v", baseAve, scoredAve)
	}
	t.Logf("damage.ave: base=%.1f scored=%.1f", *baseAve, *scoredAve)
	if !(*scoredAve > *baseAve) {
		t.Fatalf("roundhouse did not raise scored damage above auto-attack: base=%.1f scored=%.1f", *baseAve, *scoredAve)
	}

	// Per-skill breakdown must have exactly one entry named "roundhouse" with
	// non-nil damage.ave.
	if len(respScored.Combat.Skills) != 1 {
		t.Fatalf("expected 1 skill breakdown entry, got %d: %+v", len(respScored.Combat.Skills), respScored.Combat.Skills)
	}
	if respScored.Combat.Skills[0].Name != "roundhouse" {
		t.Fatalf("expected breakdown for roundhouse, got %q", respScored.Combat.Skills[0].Name)
	}
	if respScored.Combat.Skills[0].Damage.Ave == nil {
		t.Fatal("roundhouse breakdown damage.ave is nil")
	}
}
