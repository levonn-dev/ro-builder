package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_Alchemist_BuffMovesScoredField proves axe_mastery flows through
// the full path (overlay -> resolver -> contract -> skill_slot driver -> calc) and
// raises damage.ave. Axe Mastery is axe-gated, so the rig wields a 2H Axe (1360);
// the buff applies before the enemy (production order). The axe-gating negatives
// are covered by the sidecar tests. Skipped under -short. Reuses startSidecar from
// hp_buffs_e2e_test.go.
func TestIntegration_Alchemist_BuffMovesScoredField(t *testing.T) {
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
		Class:     "alchemist",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 90, Agi: 40, Vit: 40, Int: 40, Dex: 90, Luk: 20},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1360}}, // Two-Handed Axe (axe-gated)
		Skills:    []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "alchemist", "axe_mastery"), Level: 10}},
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("alchemist", snap.Skills, buffs, cat)
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
	buffed := score(t, []domain.ActiveBuff{{Name: "axe_mastery"}})
	if base.Combat.Damage.Ave == nil || buffed.Combat.Damage.Ave == nil {
		t.Fatal("damage.ave nil")
	}
	t.Logf("ave base=%.1f buffed=%.1f", *base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
	if !(*buffed.Combat.Damage.Ave > *base.Combat.Damage.Ave) {
		t.Fatalf("axe_mastery should raise damage.ave: base=%v buffed=%v",
			*base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
	}
}
