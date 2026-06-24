package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_GunslingerBuffs_RaiseScoredStats proves Single Action + Madness
// Canceller (Last Stand) flow through the full path (overlay, resolver, contract, the
// skill_slot sidecar driver, calc) and raise scored stats on a gun-wielding
// Gunslinger. Single Action raises Derived.Hit (and Aspd) with a gun equipped;
// Madness Canceller's +100 ATK raises Combat.Damage.Ave. The build equips a revolver
// (Garrison, iRO 13104). Boots its own calc-sidecar (startSidecar in
// hp_buffs_e2e_test.go). Skipped under -short.
//
// The base allocates the anchor skills but declares NO active buffs; allocation alone
// does not apply a buff (the job bank is keyed by the engine's bank id, not the Aegis id), so the
// base scores unbuffed.
func TestIntegration_GunslingerBuffs_RaiseScoredStats(t *testing.T) {
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
		Class:     "gunslinger",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 50, Agi: 90, Vit: 30, Int: 1, Dex: 90, Luk: 20},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 13104}}, // Garrison (revolver)
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "gunslinger", "single_action"), Level: 10},
			{ID: buffAnchorID(t, cat, "gunslinger", "madness_canceller"), Level: 1},
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
		{Name: "single_action"},
		{Name: "madness_canceller"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("gunslinger", buffed.Skills, buffed.ActiveBuffs, cat)
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
	t.Logf("damage.ave: base=%.1f buffed=%.1f | hit: base=%d buffed=%d | aspd: base=%.1f buffed=%.1f",
		*baseAve, *buffAve, respBase.Derived.Hit, respBuff.Derived.Hit, respBase.Derived.Aspd, respBuff.Derived.Aspd)
	if !(*buffAve > *baseAve) {
		t.Fatalf("madness_canceller did not raise damage.ave: base=%v buffed=%v", *baseAve, *buffAve)
	}
	if respBuff.Derived.Hit <= respBase.Derived.Hit {
		t.Fatalf("single_action did not raise Hit: base=%d buffed=%d", respBase.Derived.Hit, respBuff.Derived.Hit)
	}
}
