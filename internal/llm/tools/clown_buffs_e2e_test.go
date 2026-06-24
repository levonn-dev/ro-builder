package tools

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

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
