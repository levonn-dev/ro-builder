// Integration buff tests for the Expanded class group: Gunslinger, Ninja,
// Soul Linker, Star Gladiator, Super Novice.
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

// TestIntegration_Ninja_BuffMovesScoredField proves ninja_aura flows through the
// full path (overlay -> resolver -> contract -> skill_slot driver -> calc) and
// raises damage.ave. Barehanded Ninja 99/70 vs a Medium enemy; the buff applies
// before the enemy (production order). The two inert buffs are covered by the
// sidecar negatives. Skipped under -short. Reuses startSidecar from
// hp_buffs_e2e_test.go.
func TestIntegration_Ninja_BuffMovesScoredField(t *testing.T) {
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
		Class:  "ninja",
		Level:  domain.Level{Base: 99, Job: 70},
		Stats:  domain.Stats{Str: 90, Agi: 60, Vit: 40, Int: 60, Dex: 90, Luk: 40},
		Skills: []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "ninja", "ninja_aura"), Level: 5}},
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("ninja", snap.Skills, buffs, cat)
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
	buffed := score(t, []domain.ActiveBuff{{Name: "ninja_aura"}})
	if base.Combat.Damage.Ave == nil || buffed.Combat.Damage.Ave == nil {
		t.Fatal("damage.ave nil")
	}
	t.Logf("ave base=%.1f buffed=%.1f", *base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
	if !(*buffed.Combat.Damage.Ave > *base.Combat.Damage.Ave) {
		t.Fatalf("ninja_aura should raise damage.ave: base=%v buffed=%v",
			*base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
	}
}

// TestIntegration_SoulLinker_BuffMovesScoredField proves kaina flows through the
// full path (overlay -> resolver -> contract -> skill_slot driver -> calc) and
// raises maxSp (Derived.MaxSP). Soul Linker 99/70 with a Rod; buff applies before
// the enemy (production order). The 4 inert shared buffs are covered by the sidecar
// negatives. Skipped under -short. Reuses startSidecar from hp_buffs_e2e_test.go.
func TestIntegration_SoulLinker_BuffMovesScoredField(t *testing.T) {
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
		Class:     "soul_linker",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 40, Agi: 50, Vit: 50, Int: 80, Dex: 70, Luk: 30},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1601}}, // Rod
		Skills:    []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "soul_linker", "kaina"), Level: 7}},
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("soul_linker", snap.Skills, buffs, cat)
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
	buffed := score(t, []domain.ActiveBuff{{Name: "kaina"}})
	t.Logf("maxSp base=%d buffed=%d", base.Derived.MaxSP, buffed.Derived.MaxSP)
	if buffed.Derived.MaxSP <= base.Derived.MaxSP {
		t.Fatalf("kaina should raise maxSp: base=%d buffed=%d",
			base.Derived.MaxSP, buffed.Derived.MaxSP)
	}
}

// TestIntegration_StarGladiator_BuffsMoveScoredFields proves two SG self-buffs
// flow through the full path (overlay -> resolver -> contract -> skill_slot driver
// -> calc): sls_lunar_wrath raises offense (damage.ave) and sls_solar_protection
// lowers incoming damage (defense), both vs a Medium hp>=6000 enemy with a real
// attack. Barehanded build; buffs apply before the enemy (production order).
// Skipped under -short. Reuses startSidecar from hp_buffs_e2e_test.go.
func TestIntegration_StarGladiator_BuffsMoveScoredFields(t *testing.T) {
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

	build := func(buffName string, lvl int) *domain.Snapshot {
		return &domain.Snapshot{
			Class:  "star_gladiator",
			Level:  domain.Level{Base: 99, Job: 70},
			Stats:  domain.Stats{Str: 90, Agi: 30, Vit: 60, Int: 20, Dex: 90, Luk: 60},
			Skills: []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "star_gladiator", buffName), Level: lvl}},
		}
	}
	score := func(t *testing.T, snap *domain.Snapshot, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("star_gladiator", snap.Skills, buffs, cat)
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

	t.Run("lunar_wrath raises offense", func(t *testing.T) {
		snap := build("sls_lunar_wrath", 3)
		base := score(t, snap, nil)
		buffed := score(t, snap, []domain.ActiveBuff{{Name: "sls_lunar_wrath"}})
		if base.Combat.Damage.Ave == nil || buffed.Combat.Damage.Ave == nil {
			t.Fatal("damage.ave nil")
		}
		t.Logf("ave base=%.1f buffed=%.1f", *base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
		if !(*buffed.Combat.Damage.Ave > *base.Combat.Damage.Ave) {
			t.Fatalf("sls_lunar_wrath should raise damage.ave: base=%v buffed=%v",
				*base.Combat.Damage.Ave, *buffed.Combat.Damage.Ave)
		}
	})

	t.Run("solar_protection lowers incoming", func(t *testing.T) {
		snap := build("sls_solar_protection", 4)
		base := score(t, snap, nil)
		buffed := score(t, snap, []domain.ActiveBuff{{Name: "sls_solar_protection"}})
		if base.Combat.Incoming.Ave == nil || buffed.Combat.Incoming.Ave == nil {
			t.Fatal("incoming.ave nil")
		}
		t.Logf("incoming base=%.1f buffed=%.1f", *base.Combat.Incoming.Ave, *buffed.Combat.Incoming.Ave)
		if !(*buffed.Combat.Incoming.Ave < *base.Combat.Incoming.Ave) {
			t.Fatalf("sls_solar_protection should lower incoming.ave: base=%v buffed=%v",
				*base.Combat.Incoming.Ave, *buffed.Combat.Incoming.Ave)
		}
	})
}

// TestIntegration_SuperNovice_InnateBuffMovesScoredField proves the No-Death
// Bonus flows through the full path (class_innate_buffs overlay -> resolver
// innate path -> contract -> skill_slot driver -> calc) and raises maxHp (the
// +10 all stats includes +10 VIT). Super Novice 99/70 with a dagger; buff
// applies before the enemy. Skipped under -short. Reuses startSidecar.
func TestIntegration_SuperNovice_InnateBuffMovesScoredField(t *testing.T) {
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
		Class:     "super_novice",
		Level:     domain.Level{Base: 99, Job: 70},
		Stats:     domain.Stats{Str: 50, Agi: 50, Vit: 50, Int: 50, Dex: 50, Luk: 50},
		Equipment: map[domain.SlotKey]domain.EquipSpec{"weapon": {ID: 1207}}, // dagger
		// No skills allocated: no_death_bonus is innate, it needs no anchor skill.
	}
	score := func(t *testing.T, buffs []domain.ActiveBuff) *scoring.ScoreResponse {
		req := snap.ToScoreRequest(nil)
		req.EnemyInline = enemy
		if len(buffs) > 0 {
			resolved, err := resolveBuffs("super_novice", snap.Skills, buffs, cat)
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
	buffed := score(t, []domain.ActiveBuff{{Name: "no_death_bonus"}})
	t.Logf("maxHp base=%d buffed=%d", base.Derived.MaxHP, buffed.Derived.MaxHP)
	if buffed.Derived.MaxHP <= base.Derived.MaxHP {
		t.Fatalf("no_death_bonus should raise maxHp: base=%d buffed=%d",
			base.Derived.MaxHP, buffed.Derived.MaxHP)
	}
}
