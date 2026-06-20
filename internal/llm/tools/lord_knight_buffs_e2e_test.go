package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_LordKnightBuffs_RaiseScoredOffense proves the Lord Knight
// Two-Handed Sword Mastery + Twohand Quicken + Concentration chain flows through
// the full path (overlay, resolver, contract, the skill_slot sidecar driver,
// calc) and raises scored damage. Boots its own calc-sidecar (see startSidecar
// in hp_buffs_e2e_test.go). Skipped under -short.
//
// Both builds equip a Two-Handed Sword (iRO 1163 Claymore) and allocate the
// anchor skills; the base declares NO active buffs, the buffed declares the
// three. Allocation alone does not apply a buff (the job bank is keyed by rocalc
// id, not the Aegis id setSkills uses), so the base scores unbuffed and the
// increase isolates the buffs' contribution against a neutral target.
func TestIntegration_LordKnightBuffs_RaiseScoredOffense(t *testing.T) {
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
		Class:     "lord_knight",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 99, Agi: 60, Vit: 50, Int: 1, Dex: 60, Luk: 1},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1163}}, // Claymore (2H sword)
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "lord_knight", "two_handed_sword_mastery"), Level: 10},
			{ID: buffAnchorID(t, cat, "lord_knight", "twohand_quicken"), Level: 10},
			{ID: buffAnchorID(t, cat, "lord_knight", "concentration"), Level: 5},
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
		{Name: "two_handed_sword_mastery"},
		{Name: "twohand_quicken"},
		{Name: "concentration"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("lord_knight", buffed.Skills, buffed.ActiveBuffs, cat)
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
		t.Fatalf("Lord Knight buffs did not raise scored damage: base=%v buffed=%v", *baseAve, *buffAve)
	}
}
