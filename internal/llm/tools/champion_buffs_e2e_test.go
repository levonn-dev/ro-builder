package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_ChampionBuffs_RaiseScoredOffense proves the Champion Iron Fists
// + Triple Attack + Fury chain flows through the full path (overlay, resolver,
// contract, the skill_slot sidecar driver, calc) and raises scored damage. Boots
// its own calc-sidecar (see startSidecar in hp_buffs_e2e_test.go). Skipped under
// -short.
//
// Both builds equip a Knuckle (iRO 1807 Fist) and allocate the anchor skills; the
// base declares NO active buffs, the buffed declares the three. Allocation alone
// does not apply a buff (the job bank is keyed by the engine's bank id, not the Aegis id
// setSkills uses), so the base scores unbuffed. Iron Fists drives the damage.ave
// increase; Triple Attack / Fury exercise the multi-buff path (their effects land
// in secondAve / crit, which this test does not assert).
func TestIntegration_ChampionBuffs_RaiseScoredOffense(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: needs running sidecar")
	}
	baseURL, stop := startSidecar(t)
	defer stop()

	client := scoring.NewClient(baseURL, nil)
	cat := loadCat(t)

	enemy := &scoring.EnemyStats{
		Hp: 50000, AtkMin: 100, AtkMax: 200, Def: 20, MDef: 10,
		Race: "RC_DemiHuman", Element: "Ele_Neutral", ElementLv: 1, Size: "Size_Medium", Level: 80,
	}
	base := &domain.Snapshot{
		Class:     "champion",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 90, Agi: 70, Vit: 40, Int: 1, Dex: 60, Luk: 20},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1807}}, // Fist (knuckle)
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "champion", "iron_fists"), Level: 10},
			{ID: buffAnchorID(t, cat, "champion", "triple_attack"), Level: 10},
			{ID: buffAnchorID(t, cat, "champion", "fury"), Level: 5},
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
		{Name: "iron_fists"},
		{Name: "triple_attack"},
		{Name: "fury"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("champion", buffed.Skills, buffed.ActiveBuffs, cat)
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
		t.Fatalf("Champion buffs did not raise scored damage: base=%v buffed=%v", *baseAve, *buffAve)
	}
}
