package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_SinXBuffs_RaiseScoredOffense proves the Assassin Cross
// EDP + Katar Mastery chain flows through the full path (overlay, resolver,
// contract, the skill_slot sidecar driver, calc) and raises scored damage.
// Boots its own calc-sidecar (see startSidecar in hp_buffs_e2e_test.go).
// Skipped under -short.
//
// Both builds equip a Katar (iRO 1252) and allocate the anchor skills; the base
// build declares NO active buffs, the buffed build declares EDP + Katar
// Mastery. The increase is the buffs' ATK contribution, isolated from any
// element swing by attacking a neutral target with a neutral weapon.
func TestIntegration_SinXBuffs_RaiseScoredOffense(t *testing.T) {
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
		Class:     "assassin_cross",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 90, Agi: 90, Vit: 40, Int: 1, Dex: 70, Luk: 40},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1252}}, // Katar
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "assassin_cross", "enchant_deadly_poison"), Level: 5},
			{ID: buffAnchorID(t, cat, "assassin_cross", "katar_mastery"), Level: 10},
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
		{Name: "enchant_deadly_poison"},
		{Name: "katar_mastery"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("assassin_cross", buffed.Skills, buffed.ActiveBuffs, cat)
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
		t.Fatalf("EDP+katar mastery did not raise scored damage: base=%v buffed=%v", *baseAve, *buffAve)
	}
}
