package gates

import "github.com/levonn-dev/ro-builder/internal/domain"

// classGateAliases maps emulator-flavored class-name spellings to the
// canonical key used inside the gates package's lookup maps. The
// orchestrator hands snapshots whose Class is one of the
// CLASS_TO_ROCALC_ID names (lowercase-with-underscore), but the same
// concept ships under different aliases across Hercules/rAthena dumps
// and prompt-tuning data (e.g. "magician" vs "mage"). Mapping here
// keeps the per-gate maps free of duplicate entries.
//
// Kept package-local; we intentionally don't reach into
// internal/catalog for the canonical name list, per the layer-
// boundary rules. If another gate needs different normalization
// semantics later, it should add its own helper rather than bending
// this one.
var classGateAliases = map[string]string{
	"magician":      "mage",
	"high_magician": "high_mage",
	"scholar":       "professor",
}

// normalizeClassForGates returns the canonical class key for gate
// lookups. Falls through to the input unchanged when no alias applies,
// so callers can use it as a one-line lookup-key transform.
func normalizeClassForGates(class string) string {
	if alias, ok := classGateAliases[class]; ok {
		return alias
	}
	return class
}

// canonicalQualityGates returns the pre-renewal default gates used when no
// server profile is in play at all; tests, the /score profile-agnostic
// path, or a request that names no server. A profile with `quality_gates:`
// present takes over wholesale and these defaults are NOT consulted; see
// resolveQualityGates.
//
// Numbers reflect pre-renewal mechanics (95% HIT cap, 95% FLEE cap,
// 100 VIT stun immunity, etc.); a renewal server would want different
// values.
func canonicalQualityGates() domain.QualityGates {
	return domain.QualityGates{
		HIT: domain.HitGates{
			NormalFailHard: 0.80,
			NormalFail:     0.95,
			NormalWarn:     1.00,
			MVPFailHard:    0.85,
			MVPFail:        0.95,
		},
		FLEE: domain.FleeGates{
			Fail:       0.90,
			Warn:       0.95,
			WoEPenalty: 0.20,
		},
		EHPMultiplier: domain.EHPMultiplierGates{
			Normal:        1.5,
			MVPMelee:      2.0,
			MVPMagicBurst: 2.5,
		},
		TTKMVPMaxSec:   300,
		CastTimeWarnMs: 2000,
		CastTimeFailMs: 4000,
		StatusPriority: map[string]domain.StatusPriority{
			"pvm": {
				Fail: []string{"frozen", "stun"},
				Warn: []string{"stone_curse", "curse", "silence"},
			},
			"pvp": {
				Fail: []string{"frozen", "stun", "sleep"},
				Warn: []string{"stone_curse", "curse", "blind", "silence", "poison"},
			},
		},
	}
}

// resolveQualityGates returns the effective gates for an Inputs bundle.
//
// Resolution is binary, not per-field:
//
//   - profile is nil, OR profile.QualityGates is nil
//     → canonicalQualityGates() wins. The server is unauthored or the
//     caller is on the no-profile path (tests, ad-hoc /score).
//
//   - profile.QualityGates is non-nil
//     → the profile's block wins WHOLESALE. Unspecified sub-fields stay
//     at their zero values (e.g. an HIT block without NormalFail leaves
//     NormalFail=0, which the evaluator treats as "no floor").
//
// The wholesale-replacement semantics are intentional, not a gap. A
// server author who declares quality_gates is asserting full ownership of
// every threshold; silent per-field merging with canonical defaults would
// hide UARO-vs-iRO-classic semantic differences inside a fallback table
// nobody can see. Authors who want a canonical floor for a sub-field
// MUST copy it into the YAML explicitly; the YAML is the source of truth
// and the canonicalQualityGates() values are only consulted when no
// profile is configured at all.
//
// Authoring guidance: a partial quality_gates block silently leaves
// unspecified fields at zero, which the gate evaluator may interpret as
// "no floor" (e.g. EHPMultiplier.Normal=0 means any maxHp passes the
// EHP-layer-A check). ServerProfile.Validate does not currently field-
// check QualityGates, so the only place this becomes visible is at gate
// evaluation time. If you're declaring quality_gates, copy the full
// canonicalQualityGates() shape and tune the values you want different;
// don't ship a partial block expecting defaults to fill the rest.
func resolveQualityGates(profile *domain.ServerProfile) domain.QualityGates {
	if profile == nil || profile.QualityGates == nil {
		return canonicalQualityGates()
	}
	return *profile.QualityGates
}

// resolveStatusPriority returns the effective {fail, warn} status sets
// for the given scenario type, after applying request-level
// IgnoreStatus subtractions.
//
// scenarioType: "pvm" or "pvp". WoE-tier scenarios fold into pvp upstream.
//
// override is nil-safe; nil == no overrides.
func resolveStatusPriority(qg domain.QualityGates, scenarioType string, override *domain.GateOverrides) domain.StatusPriority {
	src, ok := qg.StatusPriority[scenarioType]
	if !ok {
		// Unknown scenario type; return empty (no statuses gated).
		// Callers should default to "pvm" before reaching this point.
		return domain.StatusPriority{}
	}
	if override == nil || len(override.IgnoreStatus) == 0 {
		return src
	}
	ignore := make(map[string]struct{}, len(override.IgnoreStatus))
	for _, s := range override.IgnoreStatus {
		ignore[s] = struct{}{}
	}
	return domain.StatusPriority{
		Fail: filterOut(src.Fail, ignore),
		Warn: filterOut(src.Warn, ignore),
	}
}

func filterOut(src []string, drop map[string]struct{}) []string {
	out := make([]string, 0, len(src))
	for _, s := range src {
		if _, ok := drop[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}
