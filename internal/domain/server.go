package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/levonn-dev/ro-builder/internal/data"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// serverProfileCtxKey is the context value key the orchestrator uses to
// stamp the active server profile for the duration of a request. Tools
// (lookup_monster, score_build) read it back to apply server-specific
// overlays. Defined here in the domain package (not in orchestrator)
// so tools can read it without importing orchestrator, which would
// create a cycle since orchestrator imports tools.
type serverProfileCtxKey struct{}

// WithServerProfile returns ctx with profile attached. nil profile stores
// nil; callers should pass nil when no profile applies (e.g. tests, or
// requests with no Server field).
func WithServerProfile(ctx context.Context, profile *ServerProfile) context.Context {
	return context.WithValue(ctx, serverProfileCtxKey{}, profile)
}

// ServerProfileFromContext returns the active server profile, or nil.
// Tools that need to apply overlays should call this and gracefully
// handle nil; a nil profile means "no overlay, use base catalog".
func ServerProfileFromContext(ctx context.Context) *ServerProfile {
	p, _ := ctx.Value(serverProfileCtxKey{}).(*ServerProfile)
	return p
}

// ServerProfile captures the server-specific rules an LLM needs to reason
// over a build: rates, caps, class rebalances, item changes, and ruleset
// sections for PvP/WoE. Currently ships one profile (UARO) authored by hand
// from the server's wiki; the schema is shaped to match what private-server
// wikis actually document, not to model every theoretical config knob.
//
// Profiles are embedded into the binary at compile time (see package
// configs); the orchestrator looks them up by Key when GenerateRequest.Server
// is set.
type ServerProfile struct {
	Key       string `yaml:"key" json:"key"`
	Name      string `yaml:"name" json:"name"`
	Mode      Mode   `yaml:"mode" json:"mode"`
	Episode   string `yaml:"episode,omitempty" json:"episode,omitempty"`
	Emulator  string `yaml:"emulator,omitempty" json:"emulator,omitempty"`
	SourceURL string `yaml:"source_url,omitempty" json:"source_url,omitempty"`

	Rates Rates `yaml:"rates,omitempty" json:"rates,omitempty"`
	Caps  Caps  `yaml:"caps,omitempty" json:"caps,omitempty"`

	ClassChanges []ClassChange `yaml:"class_changes,omitempty" json:"class_changes,omitempty"`
	ItemChanges  []ItemChange  `yaml:"item_changes,omitempty" json:"item_changes,omitempty"`
	GearGrants   []GearGrant   `yaml:"gear_grants,omitempty" json:"gear_grants,omitempty"`

	PvPArena    *Ruleset `yaml:"pvp_arena,omitempty" json:"pvp_arena,omitempty"`
	WoE         *Ruleset `yaml:"woe,omitempty" json:"woe,omitempty"`
	PreTransWoE *Ruleset `yaml:"pre_trans_woe,omitempty" json:"pre_trans_woe,omitempty"`

	LevelingContent *LevelingContent `yaml:"leveling_content,omitempty" json:"leveling_content,omitempty"`

	// CustomMobs is the server's overlay payload of custom or re-stat'd
	// mobs. Each entry uses the same shape as the embedded base catalog's
	// data.Mob; same iRO-id field, same combat stats. At lookup time the
	// overlay is consulted first; on a hit the overlay's mob wins, on a
	// miss we fall through to the base catalog. Re-stat'd mobs (entries
	// whose ID matches a base-catalog mob) override the base for this
	// server only.
	//
	// Inline-stats scoring path: when score_build / canonical scoring see
	// an enemy ID that's in this list, the orchestrator passes the full
	// stats blob to the calc shim (bypassing the shim's mob lookup,
	// which doesn't know about server-custom mobs).
	CustomMobs []data.Mob `yaml:"custom_mobs,omitempty" json:"custom_mobs,omitempty"`

	// QualityGates carries per-server thresholds the gates evaluator
	// consults when judging a scored snapshot. Defaults are baked into
	// each server's YAML (see configs/servers/uaro.yaml#quality_gates);
	// the gates evaluator falls back to canonical pre-re defaults if the
	// block is absent or partial. Per-request overrides flow through
	// GenerateRequest.GateOverrides.
	QualityGates *QualityGates `yaml:"quality_gates,omitempty" json:"quality_gates,omitempty"`

	Notes []string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// QualityGates is the per-server tuning block for the quality-gates
// evaluator. Every numeric field is a threshold the gate logic compares
// against the calc's ScoreResult; the StatusPriority map keys
// scenario_type ("pvm" | "pvp") to per-bucket fail/warn status lists.
//
// Pointer-on-the-parent so a server profile that omits the block emits
// nothing in JSON (vs. a struct literal that would emit a wall of zeros).
//
// Wholesale-replacement semantics; see internal/scoring/gates/defaults.go
// resolveQualityGates: when a profile declares this block, every sub-field
// it doesn't set stays at its zero value (often interpreted as "no floor"
// by the evaluator). The canonical pre-renewal defaults are NOT merged in
// per-field. If you're authoring a profile, copy the full shape from
// canonicalQualityGates() and tune; don't ship a partial block expecting
// defaults to fill the rest.
type QualityGates struct {
	HIT            HitGates                  `yaml:"hit,omitempty" json:"hit,omitempty"`
	FLEE           FleeGates                 `yaml:"flee,omitempty" json:"flee,omitempty"`
	EHPMultiplier  EHPMultiplierGates        `yaml:"ehp_multiplier,omitempty" json:"ehp_multiplier,omitempty"`
	TTKMVPMaxSec   int                       `yaml:"ttk_mvp_max_sec,omitempty" json:"ttk_mvp_max_sec,omitempty"`
	CastTimeWarnMs int                       `yaml:"cast_time_warn_ms,omitempty" json:"cast_time_warn_ms,omitempty"`
	CastTimeFailMs int                       `yaml:"cast_time_fail_ms,omitempty" json:"cast_time_fail_ms,omitempty"`
	StatusPriority map[string]StatusPriority `yaml:"status_priority,omitempty" json:"status_priority,omitempty"`
}

// HitGates: tiered HIT% thresholds. Values are fractions in [0, 1] that
// mirror combat.hit's units. NormalWarn at 1.00 means "warn whenever HIT
// falls below 100%"; every guide implicitly aims for 100% on farm
// targets.
type HitGates struct {
	NormalFailHard float64 `yaml:"normal_fail_hard,omitempty" json:"normal_fail_hard,omitempty"`
	NormalFail     float64 `yaml:"normal_fail,omitempty" json:"normal_fail,omitempty"`
	NormalWarn     float64 `yaml:"normal_warn,omitempty" json:"normal_warn,omitempty"`
	MVPFailHard    float64 `yaml:"mvp_fail_hard,omitempty" json:"mvp_fail_hard,omitempty"`
	MVPFail        float64 `yaml:"mvp_fail,omitempty" json:"mvp_fail,omitempty"`
}

// FleeGates: thresholds for the FLEE gate, plus the WoE penalty. The
// gate fires only when the build's mitigation is materially dodge-driven
// (combat.incoming.aveWithDodge < combat.incoming.ave * 0.5).
type FleeGates struct {
	Fail       float64 `yaml:"fail,omitempty" json:"fail,omitempty"`
	Warn       float64 `yaml:"warn,omitempty" json:"warn,omitempty"`
	WoEPenalty float64 `yaml:"woe_penalty,omitempty" json:"woe_penalty,omitempty"` // pre-re WoE applies -20% FLEE; gate compares against post-penalty value
}

// EHPMultiplierGates: how many times the target's biggest single hit the
// build's maxHp must absorb to avoid the burst-survival gate firing.
// Layered over the absolute "incoming.ave * battleTimeSec" floor.
type EHPMultiplierGates struct {
	Normal        float64 `yaml:"normal,omitempty" json:"normal,omitempty"`
	MVPMelee      float64 `yaml:"mvp_melee,omitempty" json:"mvp_melee,omitempty"`
	MVPMagicBurst float64 `yaml:"mvp_magic_burst,omitempty" json:"mvp_magic_burst,omitempty"`
}

// StatusPriority: per-scenario-type fail/warn lists for the
// status_immunity gate. If a target applies a status that's in `fail`
// and the build doesn't cover it, the gate emits severity=fail; if it's
// in `warn`, severity=warn; statuses outside both lists are skipped
// entirely for this scenario type.
type StatusPriority struct {
	Fail []string `yaml:"fail,omitempty" json:"fail,omitempty"`
	Warn []string `yaml:"warn,omitempty" json:"warn,omitempty"`
}

// Quality-gate severity constants. Library code rejects builds with
// any GateSeverityFail or GateSeverityFailHard result; warns flow
// through. Pass is an explicit "this gate ran and the build cleared
// it"; surfaced in the breakdown so the LLM sees the gate was
// checked.
const (
	GateSeverityPass     = "pass"
	GateSeverityWarn     = "warn"
	GateSeverityFail     = "fail"
	GateSeverityFailHard = "fail_hard"
)

// GateResult is one quality-gate emission. Threshold and Actual carry
// the values that drove the decision (number, list, or descriptor) so
// the breakdown shows near-misses as well as outright failures. Lives
// in domain (not in internal/scoring/gates) so it can land on
// Snapshot.Gates without creating an import cycle.
type GateResult struct {
	Name      string `json:"name"`
	Threshold any    `json:"threshold,omitempty"`
	Actual    any    `json:"actual,omitempty"`
	Severity  string `json:"severity"`
	Reason    string `json:"reason,omitempty"`
}

// IsPass reports whether this gate cleared with no warning and no failure.
// The single source of truth for "clean" across the orchestrator's lock
// decision and the tool's checkpoint echo, so they can't drift on what a
// passing gate is.
func (g GateResult) IsPass() bool {
	return g.Severity == GateSeverityPass
}

// IsBlocking reports whether this gate disqualifies a build (a fail or
// fail_hard). Warnings are non-blocking: they flow through as a provisional
// (replaceable) result rather than a rejection.
func (g GateResult) IsBlocking() bool {
	return g.Severity == GateSeverityFail || g.Severity == GateSeverityFailHard
}

// GateOverrides is the per-request toggle the user can supply to relax
// gates the server profile would otherwise enforce. Applied uniformly
// across all snapshots in the trajectory.
//
// IgnoreStatus is the current lever: any status key present here is dropped
// from both the fail and warn buckets of the resolved StatusPriority
// for this request. Use case: a player who knows they don't run
// frozen-heavy content saying "I don't care about frozen"; the gate
// skips the check entirely, the LLM gets that fact in its prompt and
// stops chasing frozen-immune cards. Nil/empty means "use server
// defaults verbatim."
//
// Future levers: demote_to_warn, promote_to_fail, per-snapshot scoping.
// Add fields here as the use cases show up.
type GateOverrides struct {
	IgnoreStatus []string `yaml:"ignore_status,omitempty" json:"ignore_status,omitempty"`
}

// LevelingContent describes server-specific options that affect where
// players level: repeatable quests, hunting missions, special leveling
// systems. The orchestrator surfaces this to the LLM so each Snapshot's
// leveling target reflects what the server actually offers (UARO's
// Hobota hunting missions, iRO's Eden Group, etc.) rather than generic
// pre-renewal map suggestions.
type LevelingContent struct {
	Quests []LevelingQuest `yaml:"quests,omitempty" json:"quests,omitempty"`
	Notes  []string        `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// LevelingQuest is one repeatable quest available on the server. Kinds
// captures whether the NPC offers item turn-in, monster hunt, or both;
// Rewards parallels Kinds with one entry per kind. Target is the primary
// mob name (turn-ins are for items the mob drops).
type LevelingQuest struct {
	NPC      string   `yaml:"npc" json:"npc"`
	Location string   `yaml:"location" json:"location"` // map id, e.g., "gef_fild07"
	LevelMin int      `yaml:"level_min" json:"level_min"`
	LevelMax int      `yaml:"level_max" json:"level_max"`
	Target   string   `yaml:"target" json:"target"`
	Kinds    []string `yaml:"kinds" json:"kinds"`     // "turn-in" | "hunt" | "mission"
	Rewards  []string `yaml:"rewards" json:"rewards"` // one per kind, generally
}

// Rates captures EXP/drop multipliers. Multipliers are floats so weekend
// schedules like x7.5 are representable. Zero means "not specified"; the
// renderer skips zero fields rather than emitting "x0".
type Rates struct {
	Base     float64  `yaml:"base,omitempty" json:"base,omitempty"`
	Job      float64  `yaml:"job,omitempty" json:"job,omitempty"`
	Drop     float64  `yaml:"drop,omitempty" json:"drop,omitempty"`
	CardDrop float64  `yaml:"card_drop,omitempty" json:"card_drop,omitempty"`
	MVPCard  float64  `yaml:"mvp_card,omitempty" json:"mvp_card,omitempty"`
	MVPDrop  float64  `yaml:"mvp_drop,omitempty" json:"mvp_drop,omitempty"`
	Notes    []string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Caps holds level / stat / mechanic limits relevant to build construction.
// Zero is treated as "not specified"; the renderer only emits non-zero
// fields. ASPD is float because the calc engine reports fractional values.
type Caps struct {
	MaxBaseLevel    int     `yaml:"max_base_level,omitempty" json:"max_base_level,omitempty"`
	MaxJobLevel     int     `yaml:"max_job_level,omitempty" json:"max_job_level,omitempty"`
	MaxStat         int     `yaml:"max_stat,omitempty" json:"max_stat,omitempty"`
	MaxASPD         float64 `yaml:"max_aspd,omitempty" json:"max_aspd,omitempty"`
	InstantCastDex  int     `yaml:"instant_cast_dex,omitempty" json:"instant_cast_dex,omitempty"`
	MinSkillDelayMs int     `yaml:"min_skill_delay_ms,omitempty" json:"min_skill_delay_ms,omitempty"`
	MinItemDelayMs  int     `yaml:"min_item_delay_ms,omitempty" json:"min_item_delay_ms,omitempty"`
	PartyShareRange int     `yaml:"party_share_range,omitempty" json:"party_share_range,omitempty"`
}

// ClassChange is a flat list of rule strings for one class. Class is the
// shim's class-name key (e.g. "taekwon_kid"), so the orchestrator and the
// shim's class-id table agree on identifiers.
type ClassChange struct {
	Class   string   `yaml:"class" json:"class"`
	Summary string   `yaml:"summary,omitempty" json:"summary,omitempty"`
	Rules   []string `yaml:"rules" json:"rules"`
	// MaxBaseLevel / MaxJobLevel override the catalog's pre-renewal
	// default for this class on this server. Zero means "no override;
	// use the catalog default."
	MaxBaseLevel int `yaml:"max_base_level,omitempty" json:"max_base_level,omitempty"`
	MaxJobLevel  int `yaml:"max_job_level,omitempty" json:"max_job_level,omitempty"`
}

// ItemChange records a tweak to an iRO-IDed item (stats, slot, drop source,
// equip restriction). ID is the iRO id; Name is the display name (kept for
// human readability of the YAML and prompt). Either field may be empty for
// card-only or generic notes.
//
// GrantsImmunity / GrantsUninterruptibleCast mirror the same fields on
// data.Item; same status keys (data.Immunity*), same semantics. Server
// profiles use these when they introduce custom items, restat an existing
// item to grant a new immunity, or want a single source of truth for an
// item's immunity profile under that server's rules.
type ItemChange struct {
	ID                        int      `yaml:"id,omitempty" json:"id,omitempty"`
	Name                      string   `yaml:"name,omitempty" json:"name,omitempty"`
	Notes                     []string `yaml:"notes" json:"notes"`
	GrantsImmunity            []string `yaml:"grants_immunity,omitempty" json:"grants_immunity,omitempty"`
	GrantsUninterruptibleCast bool     `yaml:"grants_uninterruptible_cast,omitempty" json:"grants_uninterruptible_cast,omitempty"`
}

// GearGrant captures a server-specific equip-restriction relaxation: a set
// of classes that gain access to a list of items they otherwise couldn't
// wear. UARO's "Extended classes can equip Trans-only gear" is the
// motivating case. Class keys should match the shim's class-id
// names so the renderer can filter by the request's class.
type GearGrant struct {
	Classes     []string `yaml:"classes" json:"classes"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Items       []string `yaml:"items" json:"items"`
}

// Ruleset is the shared shape for PvP arena, WoE, and Pre-Trans WoE
// sections. Fields not relevant to a given mode are simply left zero;
// e.g. PlayerCap is meaningful for PvPArena, MemberCap for WoE. The
// renderer emits only set fields.
type Ruleset struct {
	Schedule        string   `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	MinBaseLevel    int      `yaml:"min_base_level,omitempty" json:"min_base_level,omitempty"`
	PlayerCap       int      `yaml:"player_cap,omitempty" json:"player_cap,omitempty"`
	MemberCap       int      `yaml:"member_cap,omitempty" json:"member_cap,omitempty"`
	MinParticipants int      `yaml:"min_participants,omitempty" json:"min_participants,omitempty"`
	BannedClasses   []string `yaml:"banned_classes,omitempty" json:"banned_classes,omitempty"`
	BannedSkills    []string `yaml:"banned_skills,omitempty" json:"banned_skills,omitempty"`
	BannedItems     []string `yaml:"banned_items,omitempty" json:"banned_items,omitempty"`
	Notes           []string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// CustomMob returns the overlay entry for an iRO mob id, or nil if the
// server doesn't define or re-stat this mob. Used by the orchestrator's
// inline-stats path to detect "this enemy id needs an inline blob, not a
// calc-engine mob lookup", and by the lookup_monster tool to prefer
// the overlay before falling through to the base catalog.
func (p *ServerProfile) CustomMob(iroID int) *Mob {
	for i := range p.CustomMobs {
		if p.CustomMobs[i].ID == iroID {
			return &p.CustomMobs[i]
		}
	}
	return nil
}

// ClassMaxLevelOverride returns the server-specific max base and job
// level overrides for className, if the profile defines them via a
// class_changes entry. Either field may be zero ("no override; use
// catalog default"). The bool indicates whether ANY override matched.
func (p *ServerProfile) ClassMaxLevelOverride(className string) (maxBase, maxJob int, ok bool) {
	if p == nil {
		return 0, 0, false
	}
	for _, cc := range p.ClassChanges {
		if cc.Class != className {
			continue
		}
		if cc.MaxBaseLevel == 0 && cc.MaxJobLevel == 0 {
			return 0, 0, false
		}
		return cc.MaxBaseLevel, cc.MaxJobLevel, true
	}
	return 0, 0, false
}

// Mob is re-exported so callers can speak in domain terms without
// reaching into the data package directly.
type Mob = data.Mob

// ResolveEnemy picks the right ScoreRequest enemy fields for a given
// mob id under the active profile. If the profile defines mobID as a
// custom mob (server overlay), returns inline stats with no enemy id
// (the calc shim's setEnemyInline path). Otherwise returns the bare iRO
// id for the calc-engine mob lookup. Returns (nil, nil) when mobID is
// 0; no scenario target.
//
// Both return values mirror the mutually-exclusive shape of
// scoring.ScoreRequest's Enemy / EnemyInline pair: exactly one is
// non-nil on a real resolution.
func ResolveEnemy(profile *ServerProfile, mobID int) (enemyID *int, inline *scoring.EnemyStats) {
	if mobID == 0 {
		return nil, nil
	}
	if profile != nil {
		if cm := profile.CustomMob(mobID); cm != nil {
			return nil, mobToEnemyStats(cm)
		}
	}
	id := mobID
	return &id, nil
}

// mobToEnemyStats lifts the calc-relevant subset of data.Mob into the
// scoring contract's EnemyStats shape. Level is passed through so the
// shim's pre-renewal hit/flee scaling reflects the real target instead
// of defaulting to lvl 1.
func mobToEnemyStats(m *Mob) *scoring.EnemyStats {
	return &scoring.EnemyStats{
		Hp:        m.Hp,
		AtkMin:    m.AtkMin,
		AtkMax:    m.AtkMax,
		Def:       m.Def,
		MDef:      m.MDef,
		Race:      m.Race,
		Element:   m.Element,
		ElementLv: m.ElementLv,
		Size:      m.Size,
		Level:     m.Lv,
	}
}

// QualityGatesWarnings returns one entry per zero-valued threshold in
// the profile's QualityGates block; i.e. the silent-disable cases that
// arise from partial authoring. resolveQualityGates uses wholesale-
// replacement semantics (see internal/scoring/gates/defaults.go): when a
// profile declares quality_gates, every sub-field it doesn't set stays
// at its zero value, which the gate evaluator typically reads as "no
// floor"; effectively disabling that gate category for this server.
//
// Surfaced as warnings (not validation errors) so authors can
// intentionally zero a threshold to disable that gate; the goal is
// observability, not refusal. Callers (configs.LoadServerProfile) emit
// each entry via slog.Warn at startup.
//
// Returns nil when QualityGates is nil; the profile delegates to the
// canonical pre-renewal defaults, no partial-block risk.
func (p *ServerProfile) QualityGatesWarnings() []string {
	if p == nil || p.QualityGates == nil {
		return nil
	}
	qg := p.QualityGates
	var w []string
	if qg.HIT.NormalFailHard == 0 {
		w = append(w, "hit.normal_fail_hard")
	}
	if qg.HIT.NormalFail == 0 {
		w = append(w, "hit.normal_fail")
	}
	if qg.HIT.NormalWarn == 0 {
		w = append(w, "hit.normal_warn")
	}
	if qg.HIT.MVPFailHard == 0 {
		w = append(w, "hit.mvp_fail_hard")
	}
	if qg.HIT.MVPFail == 0 {
		w = append(w, "hit.mvp_fail")
	}
	if qg.FLEE.Fail == 0 {
		w = append(w, "flee.fail")
	}
	if qg.FLEE.Warn == 0 {
		w = append(w, "flee.warn")
	}
	if qg.FLEE.WoEPenalty == 0 {
		w = append(w, "flee.woe_penalty")
	}
	if qg.EHPMultiplier.Normal == 0 {
		w = append(w, "ehp_multiplier.normal")
	}
	if qg.EHPMultiplier.MVPMelee == 0 {
		w = append(w, "ehp_multiplier.mvp_melee")
	}
	if qg.EHPMultiplier.MVPMagicBurst == 0 {
		w = append(w, "ehp_multiplier.mvp_magic_burst")
	}
	if qg.TTKMVPMaxSec == 0 {
		w = append(w, "ttk_mvp_max_sec")
	}
	if qg.CastTimeWarnMs == 0 {
		w = append(w, "cast_time_warn_ms")
	}
	if qg.CastTimeFailMs == 0 {
		w = append(w, "cast_time_fail_ms")
	}
	if _, ok := qg.StatusPriority["pvm"]; !ok {
		w = append(w, "status_priority.pvm")
	}
	if _, ok := qg.StatusPriority["pvp"]; !ok {
		w = append(w, "status_priority.pvp")
	}
	return w
}

// Validate performs boundary-level sanity checks on a freshly-loaded
// profile. Catches the obvious authoring mistakes (missing key, negative
// caps, unknown mode) before the profile reaches the orchestrator.
func (p *ServerProfile) Validate() error {
	if p.Key == "" {
		return fmt.Errorf("server profile has empty key")
	}
	switch p.Mode {
	case "", PreRenewal, Renewal:
	default:
		return fmt.Errorf("server profile %q: unknown mode %q", p.Key, p.Mode)
	}
	if p.Caps.MaxBaseLevel < 0 || p.Caps.MaxJobLevel < 0 || p.Caps.MaxStat < 0 {
		return fmt.Errorf("server profile %q: caps must be >= 0", p.Key)
	}
	for i, cc := range p.ClassChanges {
		if cc.Class == "" {
			return fmt.Errorf("server profile %q: class_changes[%d] missing class", p.Key, i)
		}
	}
	for i, m := range p.CustomMobs {
		if m.ID <= 0 {
			return fmt.Errorf("server profile %q: custom_mobs[%d] missing or non-positive id", p.Key, i)
		}
	}
	return nil
}

// RenderForPrompt produces the human-readable block injected into the LLM's
// user message when this profile applies to the request. The shape is
// markdown-ish bulleted text; easier for the model to parse than YAML,
// and stable enough to include verbatim in fixtures.
//
// Class scopes the output: rules for the named class are listed in full;
// other classes' rules are omitted to keep the block tight. Pass an empty
// string to include every class.
func (p *ServerProfile) RenderForPrompt(class string) string {
	var sb strings.Builder

	displayName := p.Name
	if displayName == "" {
		displayName = p.Key
	}
	fmt.Fprintf(&sb, "Server profile (%s):\n", displayName)
	if p.Episode != "" {
		fmt.Fprintf(&sb, "- Episode: %s\n", p.Episode)
	}
	if p.Mode != "" {
		fmt.Fprintf(&sb, "- Mode: %s\n", p.Mode)
	}

	writeCaps(&sb, p.Caps)
	writeRates(&sb, p.Rates)
	writeClassChanges(&sb, p.ClassChanges, class)
	writeItemChanges(&sb, p.ItemChanges)
	writeGearGrants(&sb, p.GearGrants, class)
	writeRuleset(&sb, "PvP arena", p.PvPArena)
	writeRuleset(&sb, "WoE", p.WoE)
	writeRuleset(&sb, "Pre-Trans WoE", p.PreTransWoE)
	writeLevelingContent(&sb, p.LevelingContent)
	writeCustomMobs(&sb, p.CustomMobs)

	for _, n := range p.Notes {
		fmt.Fprintf(&sb, "- Note: %s\n", n)
	}

	return strings.TrimRight(sb.String(), "\n")
}

func writeCaps(sb *strings.Builder, c Caps) {
	parts := []string{}
	if c.MaxBaseLevel > 0 {
		parts = append(parts, fmt.Sprintf("base lvl %d", c.MaxBaseLevel))
	}
	if c.MaxJobLevel > 0 {
		parts = append(parts, fmt.Sprintf("job lvl %d", c.MaxJobLevel))
	}
	if c.MaxStat > 0 {
		parts = append(parts, fmt.Sprintf("max stat %d", c.MaxStat))
	}
	if c.MaxASPD > 0 {
		parts = append(parts, fmt.Sprintf("max ASPD %g", c.MaxASPD))
	}
	if c.InstantCastDex > 0 {
		parts = append(parts, fmt.Sprintf("instant cast at %d DEX", c.InstantCastDex))
	}
	if c.MinSkillDelayMs > 0 {
		parts = append(parts, fmt.Sprintf("min skill delay %dms", c.MinSkillDelayMs))
	}
	if c.MinItemDelayMs > 0 {
		parts = append(parts, fmt.Sprintf("min item delay %dms", c.MinItemDelayMs))
	}
	if c.PartyShareRange > 0 {
		parts = append(parts, fmt.Sprintf("party share ±%d lvl", c.PartyShareRange))
	}
	if len(parts) > 0 {
		fmt.Fprintf(sb, "- Caps: %s\n", strings.Join(parts, ", "))
	}
}

func writeRates(sb *strings.Builder, r Rates) {
	parts := []string{}
	if r.Base > 0 || r.Job > 0 || r.Drop > 0 {
		parts = append(parts, fmt.Sprintf("x%g/x%g/x%g base/job/drop", r.Base, r.Job, r.Drop))
	}
	if r.CardDrop > 0 {
		parts = append(parts, fmt.Sprintf("card drop x%g", r.CardDrop))
	}
	if r.MVPDrop > 0 {
		parts = append(parts, fmt.Sprintf("MVP drop x%g", r.MVPDrop))
	}
	if r.MVPCard > 0 {
		parts = append(parts, fmt.Sprintf("MVP card x%g", r.MVPCard))
	}
	if len(parts) > 0 {
		fmt.Fprintf(sb, "- Rates: %s\n", strings.Join(parts, ", "))
	}
	for _, n := range r.Notes {
		fmt.Fprintf(sb, "  - %s\n", n)
	}
}

func writeClassChanges(sb *strings.Builder, ccs []ClassChange, class string) {
	if len(ccs) == 0 {
		return
	}
	want := strings.ToLower(strings.TrimSpace(class))
	first := true
	for _, cc := range ccs {
		key := strings.ToLower(cc.Class)
		if want != "" && key != want {
			continue
		}
		if first {
			sb.WriteString("- Class changes:\n")
			first = false
		}
		fmt.Fprintf(sb, "  - %s", cc.Class)
		if cc.Summary != "" {
			fmt.Fprintf(sb, "; %s", cc.Summary)
		}
		sb.WriteString("\n")
		if cc.MaxBaseLevel > 0 {
			fmt.Fprintf(sb, "    - max base level: %d\n", cc.MaxBaseLevel)
		}
		if cc.MaxJobLevel > 0 {
			fmt.Fprintf(sb, "    - max job level: %d\n", cc.MaxJobLevel)
		}
		for _, r := range cc.Rules {
			fmt.Fprintf(sb, "    - %s\n", r)
		}
	}
}

func writeItemChanges(sb *strings.Builder, items []ItemChange) {
	if len(items) == 0 {
		return
	}
	sb.WriteString("- Item changes:\n")
	for _, it := range items {
		header := it.Name
		if it.ID > 0 && header != "" {
			header = fmt.Sprintf("%s (id %d)", it.Name, it.ID)
		} else if it.ID > 0 {
			header = fmt.Sprintf("id %d", it.ID)
		}
		if header != "" {
			fmt.Fprintf(sb, "  - %s\n", header)
		}
		for _, n := range it.Notes {
			fmt.Fprintf(sb, "    - %s\n", n)
		}
	}
}

func writeGearGrants(sb *strings.Builder, grants []GearGrant, class string) {
	if len(grants) == 0 {
		return
	}
	want := strings.ToLower(strings.TrimSpace(class))
	first := true
	for _, g := range grants {
		if want != "" && !classInList(want, g.Classes) {
			continue
		}
		if first {
			sb.WriteString("- Gear grants:\n")
			first = false
		}
		header := strings.Join(g.Classes, ", ")
		if g.Description != "" {
			fmt.Fprintf(sb, "  - %s; %s\n", header, g.Description)
		} else {
			fmt.Fprintf(sb, "  - %s\n", header)
		}
		for _, it := range g.Items {
			fmt.Fprintf(sb, "    - %s\n", it)
		}
	}
}

func writeCustomMobs(sb *strings.Builder, mobs []Mob) {
	if len(mobs) == 0 {
		return
	}
	sb.WriteString("- Custom mobs (server overlay; score_build accepts these IDs via the inline-stats path):\n")
	for _, m := range mobs {
		fmt.Fprintf(sb, "  - %s (id %d); lvl %d, %d HP",
			displayName(m), m.ID, m.Lv, m.Hp)
		if m.AtkMin != 0 || m.AtkMax != 0 {
			fmt.Fprintf(sb, ", atk %d–%d", m.AtkMin, m.AtkMax)
		}
		if m.Def != 0 || m.MDef != 0 {
			fmt.Fprintf(sb, ", def %d / mdef %d", m.Def, m.MDef)
		}
		traits := []string{}
		if m.Race != "" {
			traits = append(traits, m.Race)
		}
		if m.Element != "" {
			el := m.Element
			if m.ElementLv > 0 {
				el = fmt.Sprintf("%s lvl %d", el, m.ElementLv)
			}
			traits = append(traits, el)
		}
		if m.Size != "" {
			traits = append(traits, m.Size)
		}
		if len(traits) > 0 {
			fmt.Fprintf(sb, ", %s", strings.Join(traits, " / "))
		}
		sb.WriteString("\n")
	}
}

// displayName picks the human-readable mob name, falling back to the
// sprite/aegis identifier when Name is empty (which can happen for very
// terse custom-mob authoring).
func displayName(m Mob) string {
	if m.Name != "" {
		return m.Name
	}
	if m.SpriteName != "" {
		return m.SpriteName
	}
	return fmt.Sprintf("mob %d", m.ID)
}

func writeLevelingContent(sb *strings.Builder, lc *LevelingContent) {
	if lc == nil {
		return
	}
	if len(lc.Notes) == 0 && len(lc.Quests) == 0 {
		return
	}
	sb.WriteString("- Leveling content:\n")
	for _, n := range lc.Notes {
		fmt.Fprintf(sb, "  - %s\n", n)
	}
	if len(lc.Quests) == 0 {
		return
	}
	sb.WriteString("  - Repeatable quests:\n")
	for _, q := range lc.Quests {
		fmt.Fprintf(sb, "    - lvl %d–%d, %s @ %s; %s (%s)\n",
			q.LevelMin, q.LevelMax, q.NPC, q.Location, q.Target,
			strings.Join(q.Kinds, "+"))
		for _, r := range q.Rewards {
			fmt.Fprintf(sb, "        - %s\n", r)
		}
	}
}

func classInList(class string, list []string) bool {
	for _, c := range list {
		if strings.EqualFold(c, class) {
			return true
		}
	}
	return false
}

func writeRuleset(sb *strings.Builder, label string, r *Ruleset) {
	if r == nil {
		return
	}
	fmt.Fprintf(sb, "- %s:\n", label)
	if r.Schedule != "" {
		fmt.Fprintf(sb, "  - Schedule: %s\n", r.Schedule)
	}
	caps := []string{}
	if r.MinBaseLevel > 0 {
		caps = append(caps, fmt.Sprintf("min base lvl %d", r.MinBaseLevel))
	}
	if r.PlayerCap > 0 {
		caps = append(caps, fmt.Sprintf("player cap %d", r.PlayerCap))
	}
	if r.MemberCap > 0 {
		caps = append(caps, fmt.Sprintf("member cap %d", r.MemberCap))
	}
	if r.MinParticipants > 0 {
		caps = append(caps, fmt.Sprintf("min %d", r.MinParticipants))
	}
	if len(caps) > 0 {
		fmt.Fprintf(sb, "  - Limits: %s\n", strings.Join(caps, ", "))
	}
	if len(r.BannedClasses) > 0 {
		fmt.Fprintf(sb, "  - Banned classes: %s\n", strings.Join(r.BannedClasses, ", "))
	}
	if len(r.BannedSkills) > 0 {
		fmt.Fprintf(sb, "  - Banned skills: %s\n", strings.Join(r.BannedSkills, ", "))
	}
	if len(r.BannedItems) > 0 {
		fmt.Fprintf(sb, "  - Banned items: %s\n", strings.Join(r.BannedItems, ", "))
	}
	for _, n := range r.Notes {
		fmt.Fprintf(sb, "  - %s\n", n)
	}
}
