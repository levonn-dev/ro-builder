package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_SoulLinker_BuffMovesScoredField proves kaina flows through the
// full path (overlay -> resolver -> contract -> skill_slot driver -> calc) and
// raises maxSp (Derived.MaxSP). Soul Linker 99/70 with a Rod; buff applies before
// the enemy (production order). The 4 inert shared buffs are covered by the sidecar
// negatives. Skipped under -short. Reuses startSidecar from hp_buffs_e2e_test.go.
func TestIntegration_SoulLinker_BuffMovesScoredField(t *testing.T) {
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
		Class:     "soul_linker",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 40, Agi: 50, Vit: 50, Int: 80, Dex: 70, Luk: 30},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1601}}, // Rod
		Skills:    []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "soul_linker", "kaina"), Level: 7}},
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("soul_linker", snap.Skills, buffs, cat)
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
	buffed := score(t, []domain.ActiveBuff{{Name: "kaina"}})
	t.Logf("maxSp base=%d buffed=%d", base.Derived.MaxSP, buffed.Derived.MaxSP)
	if buffed.Derived.MaxSP <= base.Derived.MaxSP {
		t.Fatalf("kaina should raise maxSp: base=%d buffed=%d",
			base.Derived.MaxSP, buffed.Derived.MaxSP)
	}
}
