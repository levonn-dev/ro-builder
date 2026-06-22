package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_StarGladiator_BuffsMoveScoredFields proves two SG self-buffs
// flow through the full path (overlay -> resolver -> contract -> skill_slot driver
// -> calc): sls_lunar_wrath raises offense (damage.ave) and sls_solar_protection
// lowers incoming damage (defense), both vs a Medium hp>=6000 enemy with a real
// attack. Barehanded build; buffs apply before the enemy (production order).
// Skipped under -short. Reuses startSidecar from hp_buffs_e2e_test.go.
func TestIntegration_StarGladiator_BuffsMoveScoredFields(t *testing.T) {
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

	build := func(buffName string, lvl int) *domain.Snapshot {
		return &domain.Snapshot{
			Class:  "star_gladiator",
			Level:  domain.Level{Base: 99, Job: 70},
			Stats:  domain.Stats{Str: 90, Agi: 30, Vit: 60, Int: 20, Dex: 90, Luk: 60},
			Skills: []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "star_gladiator", buffName), Level: lvl}},
		}
	}
	score := func(t *testing.T, snap *domain.Snapshot, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("star_gladiator", snap.Skills, buffs, cat)
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

	t.Run("lunar_wrath raises offense", func(t *testing.T) {
		snap := build("sls_lunar_wrath", 3)
		base := score(t, snap, nil)
		buffed := score(t, snap, []domain.ActiveBuff{{Name: "sls_lunar_wrath"}})
		if base.Combat.Damage.Ave == nil || buffed.Combat.Damage.Ave == nil {
			t.Fatal("damage.ave nil")
		}
		t.Logf("ave base=%.1f buffed=%.1f", *base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
		if !(*buffed.Combat.Damage.Ave > *base.Combat.Damage.Ave) {
			t.Fatalf("sls_lunar_wrath should raise damage.ave: base=%v buffed=%v",
				*base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
		}
	})

	t.Run("solar_protection lowers incoming", func(t *testing.T) {
		snap := build("sls_solar_protection", 4)
		base := score(t, snap, nil)
		buffed := score(t, snap, []domain.ActiveBuff{{Name: "sls_solar_protection"}})
		if base.Combat.Incoming.Ave == nil || buffed.Combat.Incoming.Ave == nil {
			t.Fatal("incoming.ave nil")
		}
		t.Logf("incoming base=%.1f buffed=%.1f", *base.Combat.Incoming.Ave, *buffed.Combat.Incoming.Ave)
		if !(*buffed.Combat.Incoming.Ave < *base.Combat.Incoming.Ave) {
			t.Fatalf("sls_solar_protection should lower incoming.ave: base=%v buffed=%v",
				*base.Combat.Incoming.Ave, *buffed.Combat.Incoming.Ave)
		}
	})
}
