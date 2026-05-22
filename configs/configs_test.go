package configs

import (
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

func TestLoadServerProfile_UARO(t *testing.T) {
	p, err := LoadServerProfile("uaro")
	if err != nil {
		t.Fatalf("LoadServerProfile: %v", err)
	}

	if p.Key != "uaro" {
		t.Errorf("Key: got %q want %q", p.Key, "uaro")
	}
	if p.Mode != domain.PreRenewal {
		t.Errorf("Mode: got %q want %q", p.Mode, domain.PreRenewal)
	}

	// Caps should be the documented UARO numbers.
	if got, want := p.Caps.MaxBaseLevel, 99; got != want {
		t.Errorf("Caps.MaxBaseLevel: got %d want %d", got, want)
	}
	if got, want := p.Caps.MaxJobLevel, 70; got != want {
		t.Errorf("Caps.MaxJobLevel: got %d want %d", got, want)
	}
	if got, want := p.Caps.InstantCastDex, 150; got != want {
		t.Errorf("Caps.InstantCastDex: got %d want %d", got, want)
	}

	// Rates: x5/x5/x5 base/job/drop is the canonical UARO claim.
	if p.Rates.Base != 5 || p.Rates.Job != 5 || p.Rates.Drop != 5 {
		t.Errorf("Rates base/job/drop: got %v/%v/%v want 5/5/5",
			p.Rates.Base, p.Rates.Job, p.Rates.Drop)
	}

	// Taekwon Kid class change is the current focus class; must be present
	// and must include the mission rebalance, since the LLM uses these
	// to decide skill/stat tradeoffs.
	tk := findClassChange(p.ClassChanges, "taekwon_kid")
	if tk == nil {
		t.Fatalf("class_changes missing taekwon_kid; got classes=%v", classKeys(p.ClassChanges))
	}
	joined := strings.Join(tk.Rules, "\n")
	if !strings.Contains(joined, "Taekwon Mission") {
		t.Errorf("taekwon_kid rules should mention Taekwon Mission: %s", joined)
	}
	if !strings.Contains(joined, "tripled max HP/SP") {
		t.Errorf("taekwon_kid rules should mention the top-rank HP/SP buff: %s", joined)
	}

	// Extended-class gear grant; Trans-only gear extended to the
	// 5 expanded classes is a major build-relevant rule, not just a
	// flavor note. If this regresses, the LLM stops recommending
	// Valkyrian Helm / Sprint Mail / etc. for Taekwon builds.
	if len(p.GearGrants) == 0 {
		t.Fatal("gear_grants empty; UARO grants Trans-only gear to Extended classes")
	}

	// CustomMobs; UARO ports OGH from renewal. Ids 2464-2476 must be
	// present so the inline-stats scoring path can resolve them.
	// Spot-check Amdarais (2476) which is the headline MVP, and verify
	// inline threats parse (the gates evaluator's status_immunity gate
	// reads from this).
	if amdarais := p.CustomMob(2476); amdarais == nil {
		t.Errorf("expected OGH MVP Amdarais (2476) in custom_mobs; got %d entries", len(p.CustomMobs))
	} else {
		if amdarais.Race != "RC_Undead" {
			t.Errorf("Amdarais Race: got %q want RC_Undead", amdarais.Race)
		}
		if amdarais.Hp == 0 {
			t.Error("Amdarais HP should be non-zero")
		}
		if amdarais.Threats == nil {
			t.Error("Amdarais Threats should be populated (stun + curse from NPC_STUNATTACK + NPC_CURSEATTACK)")
		} else {
			gotStun := false
			gotCurse := false
			for _, s := range amdarais.Threats.AppliesStatus {
				if s == "stun" {
					gotStun = true
				}
				if s == "curse" {
					gotCurse = true
				}
			}
			if !gotStun || !gotCurse {
				t.Errorf("Amdarais applies_status missing stun/curse: got %v", amdarais.Threats.AppliesStatus)
			}
		}
	}
	hasTKGrant := false
	for _, g := range p.GearGrants {
		for _, c := range g.Classes {
			if c == "taekwon_kid" {
				hasTKGrant = true
				break
			}
		}
	}
	if !hasTKGrant {
		t.Errorf("expected at least one gear_grant covering taekwon_kid; got grants=%+v", p.GearGrants)
	}

	// LevelingContent; 25 repeatable quests + Hobota system note. The
	// LLM picks per-snapshot leveling targets from this list, so it must
	// load with the quest data structured (not just embedded in notes).
	if p.LevelingContent == nil {
		t.Fatal("leveling_content missing")
	}
	if len(p.LevelingContent.Quests) < 20 {
		t.Errorf("expected at least 20 repeatable quests; got %d", len(p.LevelingContent.Quests))
	}
	// Spot-check a known quest from the wiki: Cuir at cmd_fild01 (Alligator
	// hunt / Anolian Skin turn-in, lvl 45–80). If this entry breaks, the
	// schema or the wiki source has drifted.
	var hasCuir bool
	for _, q := range p.LevelingContent.Quests {
		if q.NPC == "Cuir" && q.Location == "cmd_fild01" {
			hasCuir = true
			if q.LevelMin != 45 || q.LevelMax != 80 {
				t.Errorf("Cuir level range: got %d-%d want 45-80", q.LevelMin, q.LevelMax)
			}
			if q.Target != "Alligator" {
				t.Errorf("Cuir target: got %q want Alligator", q.Target)
			}
		}
	}
	if !hasCuir {
		t.Error("expected leveling quest for Cuir @ cmd_fild01")
	}

	// PvP, WoE, Pre-Trans WoE rulesets all populated.
	if p.PvPArena == nil {
		t.Error("PvPArena missing")
	}
	if p.WoE == nil {
		t.Error("WoE missing")
	}
	if p.PreTransWoE == nil {
		t.Error("PreTransWoE missing")
	}

	// QualityGates; the gates evaluator's per-server tuning. UARO ships
	// the canonical pre-re defaults baked in; if this block goes missing
	// the gates evaluator falls back to code-side defaults but a server
	// should always carry the explicit values.
	if p.QualityGates == nil {
		t.Fatal("QualityGates missing; UARO must carry the canonical pre-re defaults")
	}
	if p.QualityGates.HIT.NormalFail != 0.95 {
		t.Errorf("HIT.NormalFail: got %v want 0.95", p.QualityGates.HIT.NormalFail)
	}
	if p.QualityGates.HIT.MVPFail != 0.95 {
		t.Errorf("HIT.MVPFail: got %v want 0.95", p.QualityGates.HIT.MVPFail)
	}
	if p.QualityGates.FLEE.Fail != 0.90 {
		t.Errorf("FLEE.Fail: got %v want 0.90", p.QualityGates.FLEE.Fail)
	}
	if p.QualityGates.EHPMultiplier.MVPMagicBurst != 2.5 {
		t.Errorf("EHPMultiplier.MVPMagicBurst: got %v want 2.5", p.QualityGates.EHPMultiplier.MVPMagicBurst)
	}
	if p.QualityGates.TTKMVPMaxSec != 300 {
		t.Errorf("TTKMVPMaxSec: got %v want 300", p.QualityGates.TTKMVPMaxSec)
	}
	pvm, ok := p.QualityGates.StatusPriority["pvm"]
	if !ok {
		t.Fatal("status_priority.pvm missing")
	}
	wantPvMFail := []string{"frozen", "stun"}
	if !equalStrings(pvm.Fail, wantPvMFail) {
		t.Errorf("status_priority.pvm.fail: got %v want %v", pvm.Fail, wantPvMFail)
	}
	pvp, ok := p.QualityGates.StatusPriority["pvp"]
	if !ok {
		t.Fatal("status_priority.pvp missing")
	}
	wantPvPFail := []string{"frozen", "stun", "sleep"}
	if !equalStrings(pvp.Fail, wantPvPFail) {
		t.Errorf("status_priority.pvp.fail: got %v want %v", pvp.Fail, wantPvPFail)
	}
}

// equalStrings is local because the configs test file is small enough
// that pulling in reflect.DeepEqual just for this would be overkill.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLoadAllServerProfiles(t *testing.T) {
	all, err := LoadAllServerProfiles()
	if err != nil {
		t.Fatalf("LoadAllServerProfiles: %v", err)
	}
	if _, ok := all["uaro"]; !ok {
		t.Errorf("uaro profile missing; got keys: %v", keys(all))
	}
}

func TestLoadServerProfile_Unknown(t *testing.T) {
	_, err := LoadServerProfile("does_not_exist")
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestRenderForPrompt_ContainsClassRules(t *testing.T) {
	p, err := LoadServerProfile("uaro")
	if err != nil {
		t.Fatalf("LoadServerProfile: %v", err)
	}
	out := p.RenderForPrompt("taekwon_kid")
	if !strings.Contains(out, "Taekwon Mission") {
		t.Errorf("rendered prompt missing taekwon_kid rule: %s", out)
	}
	// Other classes should be filtered out when class is set.
	if strings.Contains(out, "Bowling Bash") {
		t.Errorf("rendered prompt for taekwon_kid should not include knight rules: %s", out)
	}
	// Gear grants for Taekwon Kid should appear (Extended-class gear).
	if !strings.Contains(out, "Valkyrian") {
		t.Errorf("rendered prompt for taekwon_kid should include Extended gear grant: %s", out)
	}
	// Server header always present.
	if !strings.Contains(out, "uaRO") {
		t.Errorf("rendered prompt missing server name: %s", out)
	}
	// Leveling content should appear in the prompt; quests are how the
	// LLM picks per-snapshot leveling targets.
	if !strings.Contains(out, "Leveling content") {
		t.Errorf("rendered prompt missing Leveling content section: %s", out)
	}
	if !strings.Contains(out, "Cuir") {
		t.Errorf("rendered prompt missing a known leveling quest entry: %s", out)
	}
}

func TestRenderForPrompt_EmptyClassIncludesAll(t *testing.T) {
	p, err := LoadServerProfile("uaro")
	if err != nil {
		t.Fatalf("LoadServerProfile: %v", err)
	}
	out := p.RenderForPrompt("")
	if !strings.Contains(out, "Bowling Bash") {
		t.Errorf("empty-class render should include knight rules: %s", out)
	}
	if !strings.Contains(out, "Taekwon Mission") {
		t.Errorf("empty-class render should include taekwon_kid rules: %s", out)
	}
}

// Knight is not in the Extended-classes grant; the gear list should be
// filtered out for a knight request rather than dumped as noise.
func TestRenderForPrompt_GearGrantsFilteredForOtherClass(t *testing.T) {
	p, err := LoadServerProfile("uaro")
	if err != nil {
		t.Fatalf("LoadServerProfile: %v", err)
	}
	out := p.RenderForPrompt("knight")
	if strings.Contains(out, "Valkyrian Armor") {
		t.Errorf("knight render should not include Extended-class gear list: %s", out)
	}
}

func findClassChange(ccs []domain.ClassChange, key string) *domain.ClassChange {
	for i := range ccs {
		if ccs[i].Class == key {
			return &ccs[i]
		}
	}
	return nil
}

func classKeys(ccs []domain.ClassChange) []string {
	out := make([]string, 0, len(ccs))
	for _, cc := range ccs {
		out = append(out, cc.Class)
	}
	return out
}

func keys(m map[string]*domain.ServerProfile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
