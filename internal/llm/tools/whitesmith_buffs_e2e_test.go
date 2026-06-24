package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_WhitesmithBuffs_RaiseScoredOffense proves the Whitesmith Over
// Thrust + Maximize Power + Weaponry Research chain flows through the full path
// (overlay, resolver, contract, the skill_slot sidecar driver, calc) and raises
// scored damage. Boots its own calc-sidecar (see startSidecar in
// hp_buffs_e2e_test.go). Skipped under -short.
//
// Both builds equip a Two-Handed Axe (iRO 1360) and allocate the anchor skills;
// the base build declares NO active buffs, the buffed build declares the three.
// Allocation alone does not apply a buff (the job bank is keyed by the engine's bank id,
// not the Aegis id setSkills uses), so the base build scores unbuffed and the
// increase isolates the buffs' contribution against a neutral target.
func TestIntegration_WhitesmithBuffs_RaiseScoredOffense(t *testing.T) {
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
		Class:     "whitesmith",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 90, Agi: 60, Vit: 40, Int: 1, Dex: 60, Luk: 1},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1360}}, // Two-Handed Axe
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "whitesmith", "over_thrust"), Level: 5},
			{ID: buffAnchorID(t, cat, "whitesmith", "maximize_power"), Level: 5},
			{ID: buffAnchorID(t, cat, "whitesmith", "weaponry_research"), Level: 10},
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
		{Name: "over_thrust"},
		{Name: "maximize_power"},
		{Name: "weaponry_research"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("whitesmith", buffed.Skills, buffed.ActiveBuffs, cat)
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
		t.Fatalf("Whitesmith buffs did not raise scored damage: base=%v buffed=%v", *baseAve, *buffAve)
	}
}
