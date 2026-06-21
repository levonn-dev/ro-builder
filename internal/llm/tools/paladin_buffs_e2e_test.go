package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_PaladinBuffs_RaiseDerivedStats proves Faith + Spear Quicken flow
// through the full path (overlay, resolver, contract, the skill_slot sidecar driver,
// calc) and raise scored derived stats. Faith raises MaxHP; Spear Quicken raises ASPD
// (the build equips a Lance, iRO 1410, a two-handed spear -- Spear Quicken's ASPD
// applies to 2H spears only). Both read ScoreResponse.Derived, an always-present
// block, so no Combat-nil guard is needed. Boots its own calc-sidecar (startSidecar
// in hp_buffs_e2e_test.go). Skipped under -short.
//
// The base allocates the anchor skills but declares NO active buffs; allocation alone
// does not apply a buff (the job bank is keyed by rocalc id, not the Aegis id), so the
// base scores unbuffed.
func TestIntegration_PaladinBuffs_RaiseDerivedStats(t *testing.T) {
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
		Class:     "paladin",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 90, Agi: 70, Vit: 40, Int: 1, Dex: 60, Luk: 20},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1410}}, // Lance (2H spear)
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "paladin", "faith"), Level: 10},
			{ID: buffAnchorID(t, cat, "paladin", "spear_quicken"), Level: 10},
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
		{Name: "faith"},
		{Name: "spear_quicken"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("paladin", buffed.Skills, buffed.ActiveBuffs, cat)
	if err != nil {
		t.Fatal(err)
	}
	reqBuff.Buffs = resolved
	respBuff, err := client.Score(context.Background(), reqBuff)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("maxHp: base=%d buffed=%d | aspd: base=%.1f buffed=%.1f",
		respBase.Derived.MaxHP, respBuff.Derived.MaxHP, respBase.Derived.Aspd, respBuff.Derived.Aspd)
	if respBuff.Derived.MaxHP <= respBase.Derived.MaxHP {
		t.Fatalf("faith did not raise MaxHP: base=%d buffed=%d", respBase.Derived.MaxHP, respBuff.Derived.MaxHP)
	}
	if !(respBuff.Derived.Aspd > respBase.Derived.Aspd) {
		t.Fatalf("spear_quicken did not raise Aspd: base=%.1f buffed=%.1f", respBase.Derived.Aspd, respBuff.Derived.Aspd)
	}
}
