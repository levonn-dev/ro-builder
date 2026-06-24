package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_SuperNovice_InnateBuffMovesScoredField proves the No-Death
// Bonus flows through the full path (class_innate_buffs overlay -> resolver
// innate path -> contract -> skill_slot driver -> calc) and raises maxHp (the
// +10 all stats includes +10 VIT). Super Novice 99/70 with a dagger; buff
// applies before the enemy. Skipped under -short. Reuses startSidecar.
func TestIntegration_SuperNovice_InnateBuffMovesScoredField(t *testing.T) {
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
		Class:     "super_novice",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 50, Agi: 50, Vit: 50, Int: 50, Dex: 50, Luk: 50},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1207}}, // dagger
		// No skills allocated: no_death_bonus is innate, it needs no anchor skill.
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("super_novice", snap.Skills, buffs, cat)
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
	buffed := score(t, []domain.ActiveBuff{{Name: "no_death_bonus"}})
	t.Logf("maxHp base=%d buffed=%d", base.Derived.MaxHP, buffed.Derived.MaxHP)
	if buffed.Derived.MaxHP <= base.Derived.MaxHP {
		t.Fatalf("no_death_bonus should raise maxHp: base=%d buffed=%d",
			base.Derived.MaxHP, buffed.Derived.MaxHP)
	}
}
