package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_SniperBuffs_RaiseScoredOffense proves the Sniper Owl's Eye +
// Improve Concentration + True Sight chain flows through the full path (overlay,
// resolver, contract, the skill_slot sidecar driver, calc) and raises scored
// damage. Boots its own calc-sidecar (see startSidecar in hp_buffs_e2e_test.go).
// Skipped under -short.
//
// Both builds equip a Hunter Bow (iRO 1718) and allocate the anchor skills; the
// base build declares NO active buffs, the buffed build declares the three. The
// increase is the buffs' DEX/ATK contribution, isolated from any element swing
// by attacking a neutral target with a neutral weapon.
func TestIntegration_SniperBuffs_RaiseScoredOffense(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: needs running sidecar")
	}
	baseURL, stop := startSidecar(t)
	defer stop()

	client := scoring.NewClient(baseURL, nil)
	cat := loadCat(t)

	enemy := &scoring.EnemyStats{
		Hp: 50000, AtkMin: 100, AtkMax: 200, Def: 20, MDef: 10,
		Race: "RC_Brute", Element: "Ele_Neutral", ElementLv: 1, Size: "Size_Medium", Level: 80,
	}
	base := &domain.Snapshot{
		Class:     "sniper",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 1, Agi: 90, Vit: 40, Int: 1, Dex: 99, Luk: 40},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1718}}, // Hunter Bow
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "sniper", "owls_eye"), Level: 10},
			{ID: buffAnchorID(t, cat, "sniper", "improve_concentration"), Level: 10},
			{ID: buffAnchorID(t, cat, "sniper", "true_sight"), Level: 10},
		},
	}
	reqBase := base.ToScoreRequest(nil)
	reqBase.EnemyInline = enemy
	respBase, err := client.Score(context.Background(), reqBase)
	if err != nil {
		t.Fatal(err)
	}

	buffed := *base
	buffed.ActiveBuffs = []domain.ActiveBuff{
		{Name: "owls_eye"},
		{Name: "improve_concentration"},
		{Name: "true_sight"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("sniper", buffed.Skills, buffed.ActiveBuffs, cat)
	if err != nil {
		t.Fatal(err)
	}
	reqBuff.Buffs = resolved
	respBuff, err := client.Score(context.Background(), reqBuff)
	if err != nil {
		t.Fatal(err)
	}

	if respBase.Combat == nil || respBuff.Combat == nil {
		t.Fatal("combat results missing (enemy not applied?)")
	}
	baseAve, buffAve := respBase.Combat.Damage.Ave, respBuff.Combat.Damage.Ave
	if baseAve == nil || buffAve == nil {
		t.Fatalf("combat damage.ave is nil (unsolvable hit rate); raise dex/lower target def. base=%v buffed=%v", baseAve, buffAve)
	}
	t.Logf("damage.ave: base=%.1f buffed=%.1f", *baseAve, *buffAve)
	if !(*buffAve > *baseAve) {
		t.Fatalf("Sniper buffs did not raise scored damage: base=%v buffed=%v", *baseAve, *buffAve)
	}
}
