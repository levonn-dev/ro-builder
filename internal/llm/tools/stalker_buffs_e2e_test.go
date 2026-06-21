package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_StalkerBuffs_RaiseScoredStats proves Stealth (Chase Walk) + Close
// Confine flow through the full path (overlay, resolver, contract, the skill_slot
// sidecar driver, calc) and raise scored stats. Stealth's STR bonus raises
// Combat.Damage.Ave; Close Confine raises Derived.Flee. The build equips a dagger
// (Main Gauche, iRO 1207). Boots its own calc-sidecar (startSidecar in
// hp_buffs_e2e_test.go). Skipped under -short.
//
// The base allocates the anchor skills but declares NO active buffs; allocation alone
// does not apply a buff (the job bank is keyed by rocalc id, not the Aegis id), so the
// base scores unbuffed.
func TestIntegration_StalkerBuffs_RaiseScoredStats(t *testing.T) {
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
		Class:     "stalker",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 70, Agi: 90, Vit: 30, Int: 1, Dex: 70, Luk: 20},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1207}}, // Main Gauche (dagger)
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "stalker", "stealth"), Level: 5},
			{ID: buffAnchorID(t, cat, "stalker", "close_confine"), Level: 1},
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
		{Name: "stealth"},
		{Name: "close_confine"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("stalker", buffed.Skills, buffed.ActiveBuffs, cat)
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
	t.Logf("damage.ave: base=%.1f buffed=%.1f | flee: base=%d buffed=%d",
		*baseAve, *buffAve, respBase.Derived.Flee, respBuff.Derived.Flee)
	if !(*buffAve > *baseAve) {
		t.Fatalf("stealth did not raise damage.ave: base=%v buffed=%v", *baseAve, *buffAve)
	}
	if !(respBuff.Derived.Flee > respBase.Derived.Flee) {
		t.Fatalf("close_confine did not raise Flee: base=%d buffed=%d", respBase.Derived.Flee, respBuff.Derived.Flee)
	}
}
