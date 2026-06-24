package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_HighWizard_BuffMovesScoredField proves soul_drain flows through the
// full path (overlay -> resolver -> contract -> skill_slot driver -> calc) and raises
// maxSp (Derived.MaxSP). High Wizard 99/70 with a Rod; buff applies before the enemy
// (production order). mystical_amplification (derived MATK) and the inert
// increase_sp_recovery are covered by the sidecar tests. Skipped under -short. Reuses
// startSidecar from hp_buffs_e2e_test.go.
func TestIntegration_HighWizard_BuffMovesScoredField(t *testing.T) {
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
		Class:     "high_wizard",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 1, Agi: 1, Vit: 50, Int: 99, Dex: 90, Luk: 1},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1601}}, // Rod
		Skills:    []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "high_wizard", "soul_drain"), Level: 10}},
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("high_wizard", snap.Skills, buffs, cat)
			if err != nil {
				t.Fatal(err)
			}
			req.Buffs = resolved
		}
		resp, err := client.Score(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	base := score(t, nil)
	buffed := score(t, []domain.ActiveBuff{{Name: "soul_drain"}})
	t.Logf("maxSp base=%d buffed=%d", base.Derived.MaxSP, buffed.Derived.MaxSP)
	if buffed.Derived.MaxSP <= base.Derived.MaxSP {
		t.Fatalf("soul_drain should raise maxSp: base=%d buffed=%d",
			base.Derived.MaxSP, buffed.Derived.MaxSP)
	}
}
