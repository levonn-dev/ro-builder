// Integration buff tests for the standard/transcendent job-tree classes:
// Champion, Clown/Gypsy, High Wizard, Lord Knight, Paladin, Professor/Scholar,
// Assassin Cross, Sniper, Stalker, Whitesmith, and Alchemist.
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

// TestIntegration_ClownGypsyLessons_RaiseScoredOffense proves the two performer
// passives flow through the full path (overlay, resolver, contract, skill_slot
// sidecar driver, calc) and raise Combat.Damage.Ave: Musical Lesson with an
// Instrument (Clown + Violin 1901), Dancing Lesson with a Whip (Gypsy + Rope 1950).
// The base allocates the anchor skill but declares no active buff, so it scores
// unbuffed (the job bank is keyed by the engine's bank id, not the Aegis id). Skipped under -short.
func TestIntegration_ClownGypsyLessons_RaiseScoredOffense(t *testing.T) {
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
	cases := []struct {
		name   string
		class  string
		weapon int
		buff   string
		anchor string
	}{
		{"clown_musical_lesson", "clown", 1901, "musical_lesson", "musical_lesson"},
		{"gypsy_dancing_lesson", "gypsy", 1950, "dancing_lesson", "dancing_lesson"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := &domain.Snapshot{
				Class:     tc.class,
				Level:     domain.Level{Base: 99, Job: 70},
				Stats:     domain.Stats{Str: 80, Agi: 70, Vit: 30, Int: 30, Dex: 90, Luk: 20},
				Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: tc.weapon}},
				Skills:    []domain.SkillAlloc{{ID: buffAnchorID(t, cat, tc.class, tc.anchor), Level: 10}},
			}
			reqBase := base.ToScoreRequest(nil)
			reqBase.EnemyInline = enemy
			respBase, err := client.Score(context.Background(), reqBase)
			if err != nil {
				t.Fatal(err)
			}
			buffed := *base
			buffed.ActiveBuffs = []domain.ActiveBuff{{Name: tc.buff}}
			reqBuff := buffed.ToScoreRequest(nil)
			reqBuff.EnemyInline = enemy
			resolved, err := resolveBuffs(tc.class, buffed.Skills, buffed.ActiveBuffs, cat)
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
				t.Fatalf("combat damage.ave is nil; base=%v buffed=%v", baseAve, buffAve)
			}
			t.Logf("%s damage.ave: base=%.1f buffed=%.1f", tc.name, *baseAve, *buffAve)
			if !(*buffAve > *baseAve) {
				t.Fatalf("%s did not raise damage.ave: base=%v buffed=%v", tc.buff, *baseAve, *buffAve)
			}
		})
	}
}

// TestIntegration_HighWizard_BuffMovesScoredField proves soul_drain flows through the
// full path (overlay -> resolver -> contract -> skill_slot driver -> calc) and raises
// maxSp (Derived.MaxSP). High Wizard 99/70 with a Rod; buff applies before the enemy
// (production order). mystical_amplification (derived MATK) and the inert
// increase_sp_recovery are covered by the sidecar tests. Skipped under -short. Reuses
// startSidecar from hp_buffs_e2e_test.go.
func TestIntegration_HighWizard_BuffMovesScoredField(t *testing.T) {
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
	snap := &domain.Snapshot{
		Class:     "high_wizard",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 1, Agi: 1, Vit: 50, Int: 99, Dex: 90, Luk: 1},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1601}}, // Rod
		Skills:    []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "high_wizard", "soul_drain"), Level: 10}},
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("high_wizard", snap.Skills, buffs, cat)
			if err != nil {
				t.Fatal(err)
			}
			req.Buffs = resolved
		}
		resp, err := client.Score(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	base := score(t, nil)
	buffed := score(t, []domain.ActiveBuff{{Name: "soul_drain"}})
	t.Logf("maxSp base=%d buffed=%d", base.Derived.MaxSP, buffed.Derived.MaxSP)
	if buffed.Derived.MaxSP <= base.Derived.MaxSP {
		t.Fatalf("soul_drain should raise maxSp: base=%d buffed=%d",
			base.Derived.MaxSP, buffed.Derived.MaxSP)
	}
}

// TestIntegration_LordKnightBuffs_RaiseScoredOffense proves the Lord Knight
// Two-Handed Sword Mastery + Twohand Quicken + Concentration chain flows through
// the full path (overlay, resolver, contract, the skill_slot sidecar driver,
// calc) and raises scored damage. Boots its own calc-sidecar (see startSidecar
// in hp_buffs_e2e_test.go). Skipped under -short.
//
// Both builds equip a Two-Handed Sword (iRO 1163 Claymore) and allocate the
// anchor skills; the base declares NO active buffs, the buffed declares the
// three. Allocation alone does not apply a buff (the job bank is keyed by the
// engine's bank id, not the Aegis id setSkills uses), so the base scores unbuffed and the
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

// TestIntegration_PaladinBuffs_RaiseDerivedStats proves Faith + Spear Quicken flow
// through the full path (overlay, resolver, contract, the skill_slot sidecar driver,
// calc) and raise scored derived stats. Faith raises MaxHP; Spear Quicken raises ASPD
// (the build equips a Lance, iRO 1410, a two-handed spear -- Spear Quicken's ASPD
// applies to 2H spears only). Both read ScoreResponse.Derived, an always-present
// block, so no Combat-nil guard is needed. Boots its own calc-sidecar (startSidecar
// in hp_buffs_e2e_test.go). Skipped under -short.
//
// The base allocates the anchor skills but declares NO active buffs; allocation alone
// does not apply a buff (the job bank is keyed by the engine's bank id, not the Aegis id), so the
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

// TestIntegration_StalkerBuffs_RaiseScoredStats proves Stealth (Chase Walk) + Close
// Confine flow through the full path (overlay, resolver, contract, the skill_slot
// sidecar driver, calc) and raise scored stats. Stealth's STR bonus raises
// Combat.Damage.Ave; Close Confine raises Derived.Flee. The build equips a dagger
// (Main Gauche, iRO 1207). Boots its own calc-sidecar (startSidecar in
// hp_buffs_e2e_test.go). Skipped under -short.
//
// The base allocates the anchor skills but declares NO active buffs; allocation alone
// does not apply a buff (the job bank is keyed by the engine's bank id, not the Aegis id), so the
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
	if respBuff.Derived.Flee <= respBase.Derived.Flee {
		t.Fatalf("close_confine did not raise Flee: base=%d buffed=%d", respBase.Derived.Flee, respBuff.Derived.Flee)
	}
}

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

// TestIntegration_Alchemist_BuffMovesScoredField proves axe_mastery flows through
// the full path (overlay -> resolver -> contract -> skill_slot driver -> calc) and
// raises damage.ave. Axe Mastery is axe-gated, so the rig wields a 2H Axe (1360);
// the buff applies before the enemy (production order). The axe-gating negatives
// are covered by the sidecar tests. Skipped under -short. Reuses startSidecar from
// hp_buffs_e2e_test.go.
func TestIntegration_Alchemist_BuffMovesScoredField(t *testing.T) {
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
	snap := &domain.Snapshot{
		Class:     "alchemist",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 90, Agi: 40, Vit: 40, Int: 40, Dex: 90, Luk: 20},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1360}}, // Two-Handed Axe (axe-gated)
		Skills:    []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "alchemist", "axe_mastery"), Level: 10}},
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("alchemist", snap.Skills, buffs, cat)
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

	base := score(t, nil)
	buffed := score(t, []domain.ActiveBuff{{Name: "axe_mastery"}})
	if base.Combat.Damage.Ave == nil || buffed.Combat.Damage.Ave == nil {
		t.Fatal("damage.ave nil")
	}
	t.Logf("ave base=%.1f buffed=%.1f", *base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
	if !(*buffed.Combat.Damage.Ave > *base.Combat.Damage.Ave) {
		t.Fatalf("axe_mastery should raise damage.ave: base=%v buffed=%v",
			*base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
	}
}
