// Package domain holds the orchestrator-facing types: what a build is, what
// a scenario is, what a server profile is. These are the shapes the LLM
// reasons over and that flow through the API boundary; kept separate from
// scoring's HTTP-contract types so orchestrator metadata (mode, server,
// tier) doesn't leak into the calc sidecar's request schema.
//
// The build/equipment field types are aliased to scoring's mirrors of the
// TS contract; same shapes, no double-definition. A Build is conceptually
// "a ScoreRequest plus orchestrator metadata"; ToScoreRequest peels the
// metadata off and hands the calc-relevant subset to the sidecar client.
package domain

import (
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// Mode picks pre-renewal vs renewal mechanics. Only pre-renewal is currently
// supported at the API boundary; see Validate(). The field still travels
// through the stack so flipping renewal on once the calc shim is validated
// for it is a config change, not a refactor.
type Mode string

const (
	PreRenewal Mode = "pre-renewal"
	Renewal    Mode = "renewal"
)

// Tier labels which budget bracket a build represents in the response set.
// Empty string is valid and means "tier not specified"; useful for ad-hoc
// scoring requests that aren't part of a tiered build sweep.
type Tier string

const (
	TierBudget    Tier = "budget"
	TierRealistic Tier = "realistic"
	TierEndgame   Tier = "endgame"
)

// Type aliases to scoring's mirrors of the TS contract. Stats / Level /
// EquipSpec / SlotKey have identical shape on both sides; the alias avoids
// a parallel set of types and a conversion layer that would only copy
// fields one-for-one.
type (
	Stats     = scoring.Stats
	Level     = scoring.Level
	EquipSpec = scoring.EquipSpec
	SlotKey   = scoring.SlotKey
)

// Build is one candidate character setup the orchestrator scores. The
// LLM constructs Builds from class/server/scenario context; users can also
// POST one directly via /score for ad-hoc calculator-style use. Mode,
// Server, and Tier are orchestrator-side metadata and don't cross the
// shim boundary.
//
// Class is a class name per the shim contract; "novice", "taekwon_kid",
// "knight", etc. Empty defaults to Novice. The shim validates and returns
// a 4xx for unknown names.
type Build struct {
	Mode      Mode                  `json:"mode,omitempty"`
	Server    string                `json:"server,omitempty"`
	Tier      Tier                  `json:"tier,omitempty"`
	Class     string                `json:"class,omitempty"`
	Level     Level                 `json:"level"`
	Stats     Stats                 `json:"stats"`
	Equipment map[SlotKey]EquipSpec `json:"equipment,omitempty"`
}

// Validate returns an error if the build violates an invariant the API
// boundary should reject before any calc work happens. The checks here are
// boundary-level only; the shim catches richer rules (unknown class name,
// status point cap, class-can-wear-the-gear) and surfaces them via the
// 4xx path.
func (b *Build) Validate() error {
	switch b.Mode {
	case "", PreRenewal:
		// PreRenewal is the current happy path; "" is treated as PreRenewal
		// at the conversion layer so old callers don't have to set the field.
	case Renewal:
		return fmt.Errorf("mode %q not supported: only pre-renewal", b.Mode)
	default:
		return fmt.Errorf("unknown mode %q (valid: pre-renewal)", b.Mode)
	}
	if b.Level.Base < 0 || b.Level.Job < 0 {
		return fmt.Errorf("level base=%d job=%d: must be >= 0", b.Level.Base, b.Level.Job)
	}
	for slot, spec := range b.Equipment {
		if spec.Refine < 0 || spec.Refine > 10 {
			return fmt.Errorf("equipment[%s] refine %d out of range (0..10)", slot, spec.Refine)
		}
	}
	return nil
}

// ToScoreRequest converts a Build (plus optional Scenario) into the body
// the calc shim expects. Mode/Server/Tier are dropped; they're orchestrator
// metadata, not calc inputs. Zero-valued Level / Stats are sent as nil
// pointers so the shim's reset baseline (Novice / lvl 1 / stats 1) takes
// over rather than being clobbered with explicit zeros. Empty Class is
// likewise sent as nil so the shim's default applies.
//
// profile, when non-nil, swaps Scenario.Target into an inline-stats blob
// if the id matches a CustomMobs entry; stock targets pass through as
// the iRO id for calc lookup. Pass nil profile to disable overlay
// resolution (treat every target as a stock id).
func (b *Build) ToScoreRequest(s *Scenario, profile *ServerProfile) *scoring.ScoreRequest {
	req := &scoring.ScoreRequest{}
	if b.Class != "" {
		req.Class = new(b.Class)
	}
	if b.Level != (Level{}) {
		req.Level = new(b.Level)
	}
	if b.Stats != (Stats{}) {
		req.Stats = new(b.Stats)
	}
	if len(b.Equipment) > 0 {
		req.Equipment = b.Equipment
	}
	if s != nil && s.Target != 0 {
		req.Enemy, req.EnemyInline = ResolveEnemy(profile, s.Target)
	}
	return req
}
