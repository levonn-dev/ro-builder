package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_Ninja_BuffMovesScoredField proves ninja_aura flows through the
// full path (overlay -> resolver -> contract -> skill_slot driver -> calc) and
// raises damage.ave. Barehanded Ninja 99/70 vs a Medium enemy; the buff applies
// before the enemy (production order). The two inert buffs are covered by the
// sidecar negatives. Skipped under -short. Reuses startSidecar from
// hp_buffs_e2e_test.go.
func TestIntegration_Ninja_BuffMovesScoredField(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: needs running sidecar")
	}
	baseURL, stop := startSidecar(t)
	defer stop()
	client := scoring.NewClient(baseURL, nil)
	cat := loadCat(t)

	enemy := &scoring.EnemyStats{
		Hp: 50000, AtkMin: 1500, AtkMax: 2000, Def: 20, MDef: 10,
		Race: "RC_Brute", Element: "Ele_Neutral", ElementLv: 1, Size: "Size_Medium", Level: 90,
	}
	snap := &domain.Snapshot{
		Class:  "ninja",
		Level:  domain.Level{Base: 99, Job: 70},
		Stats:  domain.Stats{Str: 90, Agi: 60, Vit: 40, Int: 60, Dex: 90, Luk: 40},
		Skills: []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "ninja", "ninja_aura"), Level: 5}},
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("ninja", snap.Skills, buffs, cat)
			if err != nil {
				t.Fatal(err)
			}
			req.Buffs = resolved
		}
		resp, err := client.Score(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Combat == nil {
			t.Fatal("combat results missing (enemy not applied?)")
		}
		return resp
	}

	base := score(t, nil)
	buffed := score(t, []domain.ActiveBuff{{Name: "ninja_aura"}})
	if base.Combat.Damage.Ave == nil || buffed.Combat.Damage.Ave == nil {
		t.Fatal("damage.ave nil")
	}
	t.Logf("ave base=%.1f buffed=%.1f", *base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
	if !(*buffed.Combat.Damage.Ave > *base.Combat.Damage.Ave) {
		t.Fatalf("ninja_aura should raise damage.ave: base=%v buffed=%v",
			*base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
	}
}
