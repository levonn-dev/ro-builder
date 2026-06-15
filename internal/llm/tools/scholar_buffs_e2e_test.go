package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// TestIntegration_ScholarBuffs_RaiseScoredOffense proves the Professor
// endow + land buff chain (flame_launcher + volcano) flows through the full
// path -- overlay, resolver, contract, the land_buff sidecar driver, calc --
// and raises scored damage. Boots its own calc-sidecar (see startSidecar in
// hp_buffs_e2e_test.go). Skipped under -short.
//
// Mechanism: the base build allocates the anchor skills but declares NO
// active buffs, so it attacks with a neutral fist. The buffed build applies
// the fire endow (so the attack becomes fire) plus Volcano, whose fire branch
// then amplifies that now-fire damage. Against a neutral target both fire and
// neutral hit ~100%, so the increase comes from Volcano's amplification, not
// an element-vs-element swing.
func TestIntegration_ScholarBuffs_RaiseScoredOffense(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: needs running sidecar")
	}
	baseURL, stop := startSidecar(t)
	defer stop()

	client := scoring.NewClient(baseURL, nil)
	cat := loadCat(t)

	// Neutral target; the fire endow + Volcano scale the player's fire output.
	enemy := &scoring.EnemyStats{
		Hp: 50000, AtkMin: 100, AtkMax: 200, Def: 20, MDef: 10,
		Race: "RC_Brute", Element: "Ele_Neutral", ElementLv: 1, Size: "Size_Medium", Level: 80,
	}
	base := &domain.Snapshot{
		Class: "professor",
		Level: domain.Level{Base: 99, Job: 50},
		Stats: domain.Stats{Str: 80, Agi: 40, Vit: 40, Int: 60, Dex: 80, Luk: 20},
		Skills: []domain.SkillAlloc{
			{ID: buffAnchorID(t, cat, "professor", "flame_launcher"), Level: 5},
			{ID: buffAnchorID(t, cat, "professor", "volcano"), Level: 5},
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
		{Name: "flame_launcher", Element: "fire"},
		{Name: "volcano"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("professor", buffed.Skills, buffed.ActiveBuffs, cat)
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
	// CombatDamage.Ave is *float64 (nil when the hit rate is unsolvable).
	baseAve, buffAve := respBase.Combat.Damage.Ave, respBuff.Combat.Damage.Ave
	if baseAve == nil || buffAve == nil {
		t.Fatalf("combat damage.ave is nil (unsolvable hit rate); raise dex/lower target def. base=%v buffed=%v", baseAve, buffAve)
	}
	t.Logf("damage.ave: base=%.1f buffed=%.1f", *baseAve, *buffAve)
	if !(*buffAve > *baseAve) {
		t.Fatalf("endow+land did not raise scored damage: base=%v buffed=%v", *baseAve, *buffAve)
	}
}
