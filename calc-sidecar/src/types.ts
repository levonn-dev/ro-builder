// Shared types for the calc-sidecar.
//
// These are the public contract: anything that crosses the shim boundary,
// the score() boundary, or the HTTP boundary references types in this file.
// Internal jsdom / rocalc plumbing inside src/backends/<engine>/ stays
// loosely typed (Node's strip mode just removes annotations at runtime, so
// `any` escape hatches don't cost us anything).

/** Equipment slot keys exposed to callers. Matches the SLOTS table inside
 * the rocalc backend; any new backend MUST cover these slots. */
export type SlotKey =
  | "weapon"
  | "headTop"
  | "headMid"
  | "headBot"
  | "shield"
  | "armor"
  | "garment"
  | "footgear"
  | "accessory1"
  | "accessory2";

export interface Stats {
  str: number;
  agi: number;
  vit: number;
  int: number;
  dex: number;
  luk: number;
}

export interface Level {
  base: number;
  job: number;
}

/** One equipment-slot specification. id and cards are iRO IDs (the active
 * backend translates to its own internal id space); refine is 0-10 and
 * meaningful only for slots that accept it. */
export interface EquipSpec {
  id: number;
  refine?: number;
  cards?: number[];
}

/** One entry in a build's skill allocation: the iRO skill id and the
 * level invested. Skill IDs are Gravity-canonical (same in iRO, Hercules,
 * rAthena, rocalc) so no translation table is needed for skills the way
 * one is needed for items / mobs / cards. */
export interface SkillAlloc {
  id: number;
  level: number;
}

/** One active self-buff, fully resolved by the orchestrator (level filled
 * from the anchor skill's allocation; element validated). The active backend
 * maps `name` to its own engine controls via a binding table. `level` is the
 * resolved buff level (for level-select buffs); `element` is the chosen
 * weapon element for endow buffs (one of the backend's element names). */
export interface Buff {
  name: string;
  level?: number;
  element?: string;
}

/** Composite stat output for paired numbers; atk = base + plus, matk =
 * min..max, def = hard + soft (pre-renewal split). */
export interface BasePlus {
  base: number;
  plus: number;
}

export interface MinMax {
  min: number;
  max: number;
}

export interface HardSoft {
  hard: number;
  soft: number;
}

export interface DerivedStats {
  hit: number;
  flee: number;
  cri: number;
  atk: BasePlus;
  matk: MinMax;
  def: HardSoft;
  mdef: HardSoft;
  aspd: number;
  maxHp: number;
  maxSp: number;
  /** Stat-point pool remaining; goes negative when the build is
   * over-budget (rocalc's BLVauto is disabled by the shim). Callers
   * should treat negative values as "build is invalid for this level". */
  statPointsRemaining: number;
}

/** Combat-sim output. Numeric fields are `number | null` because rocalc
 * renders sentinel strings ("Infinite (no 100% hit)", "Too high to
 * calculate", "Over 10000 hits") for unsolvable builds; the shim returns
 * null in those cases instead of NaN. */
export interface CombatDamage {
  min: number | null;
  ave: number | null;
  max: number | null;
  /** Average damage from second-attack mechanics (Double Attack, Hindsight
   * Auto-Attack etc). null when not applicable. */
  secondAve: number | null;
}

export interface CombatCrit {
  damage: number | null;
  rate: number | null;
}

export interface CombatIncoming {
  min: number | null;
  ave: number | null;
  max: number | null;
  aveWithDodge: number | null;
}

export interface CombatEnemy {
  hp: number | null;
  race: string;
  element: string;
  size: string;
  type: string;
}

export interface CombatResults {
  damage: CombatDamage;
  crit: CombatCrit;
  /** Player's hit ratio; % chance an attack lands on the target. Sourced
   * from rocalc's BattleHIT span. */
  hit: number | null;
  /** Player's dodge ratio; % chance to dodge the target's attacks.
   * Sourced from rocalc's "Player's Dodge Ratio" row. This is the dodge
   * outcome (uses the FLEE stat plus AGI/luck and the target's HIT); the
   * flat FLEE stat itself lives on DerivedStats.flee, not here. */
  dodge: number | null;
  battleTimeSec: number | null;
  incoming: CombatIncoming;
  enemy: CombatEnemy;
}

/** The shim contract every backend implements. Constructed via createShim()
 * (each backend exports a createShim() factory matching this shape). */
export interface ShimSession {
  /** Set the active character class by name. Names follow the standard
   * pre-renewal RO labels (lowercase, underscore-separated): "novice",
   * "swordman", "magician", "knight", "wizard", "taekwon_kid",
   * "star_gladiator", "soul_linker", etc. Backends are responsible for
   * accepting common aliases ("mage" → magician, "swordsman" → swordman)
   * and normalizing case/separator. Throws on unknown names with a
   * 4xx-classifiable error (the message contains "unknown class"). */
  setClass(className: string): void;

  setLevel(level: Level): void;

  setStats(stats: Stats): void;

  /** Apply an equipment slot. id and cards are iRO IDs translated to the
   * backend's internal id space via its mapping table. */
  equip(slot: SlotKey, spec: EquipSpec): void;

  /** Set skill levels for the active class. Each entry's id is an iRO
   * skill id; level is the skill level invested (1..max). Skills not in
   * the active class's tree throw a 4xx-classifiable error. Skills not
   * passed retain their default 0; callers should pass the full
   * allocation, not a delta. */
  setSkills(skills: SkillAlloc[]): void;

  /** Apply active self-buffs. Each buff's `name` is a semantic key the
   * backend maps to engine controls (e.g. drive an m_JobBuff slot, force a
   * weapon element). Unknown names throw a 4xx-classifiable error. Buffs not
   * passed are absent; callers pass the full set, not a delta. Runs after
   * setSkills/equip and before any read. */
  setBuffs(buffs: Buff[]): void;

  /** Set the combat-sim target. mobId is engine-specific (rocalc currently
   * uses its m_Monster index; a mob iRO mapping pass will let this take iRO
   * mob ids the same way equip does for items). */
  setEnemy(mobId: number): void;

  /** Set the combat-sim target inline, using a stats blob instead of an
   * mob id lookup. Used for server-overlay custom mobs that aren't in
   * the rocalc snapshot. The shim writes the inline stats into a
   * scratch m_Monster slot and points B_Enemy at it before recomputing. */
  setEnemyInline(stats: EnemyStats): void;

  readDerivedStats(): DerivedStats;

  readCombatResults(): CombatResults;

  /** Restore the session to its post-create / post-setClass baseline.
   * Cheap (no rocalc reload, no class re-init) so HTTP servers and test
   * suites can share one createShim() across many calls. */
  reset(): void;
}

// HTTP boundary types; what the Go orchestrator sends and receives.

/** POST /score request body. All fields optional; missing fields keep the
 * shim's reset baseline (Novice / lvl 1 / all stats 1 / no equipment). */
export interface ScoreRequest {
  /** Class name; see ShimSession.setClass for accepted values. Empty
   * string or absent → Novice (the shim's default state). */
  class?: string;
  level?: Level;
  stats?: Stats;
  skills?: SkillAlloc[];
  buffs?: Buff[];
  equipment?: Partial<Record<SlotKey, EquipSpec>>;
  /** iRO mob id translated through rocalc's mapping table. Mutually
   * exclusive with enemy_inline; set one or neither. */
  enemy?: number;
  /** Inline stats for a custom mob the rocalc snapshot doesn't know
   * about (server-overlay path). Mutually exclusive with enemy. */
  enemy_inline?: EnemyStats;
}

/** Inline mob stats for the score_build's enemy_inline path. Used for
 * server-specific custom mobs that aren't in rocalc's m_Monster table.
 * Field conventions mirror Hercules's mob_db: race "RC_Plant", element
 * "Ele_Water", size "Size_Medium". */
export interface EnemyStats {
  hp: number;
  atk_min: number;
  atk_max: number;
  def: number;
  mdef: number;
  race: string;
  element: string;
  element_lv: number;
  size: string;
  /** Mob level. Pre-renewal hit/flee scaling uses this; without it,
   * high-level custom mobs (UARO's OGH 95-100) get scored as level 1
   * and the player's hit rate is overestimated by 10-20pp. Optional
   * for backwards compat; defaults to 1 when absent or non-positive. */
  level?: number;
}

export interface ScoreResponse {
  derived: DerivedStats;
  combat?: CombatResults;
  /** Version stamp of the calc engine that produced this response. The Go
   * build library persists this so stale saved trajectories are detectable
   * after the calc snapshot changes. Format is backend-defined; for the
   * rocalc backend it's the dated snapshot id ("rocalc-2026-04-06"). */
  calc_version: string;
  /** Caveats the orchestrator/LLM should weigh when reasoning over the
   * numbers in this response. The rocalc backend surfaces its known
   * issues here (left-hand crit damage, Soul Breaker / Grimtooth
   * approximations, inline-mob level scaling) so callers don't treat
   * approximated values as ground truth. Each entry is a short
   * human-readable string; absent / empty means "no known caveats
   * for this response shape". */
  uncertainty?: string[];
  /** Opaque per-response identifier for debugging; surfaces in logs and
   * the build library so a saved trajectory can be tied back to the
   * exact calc invocation that produced its scores. Backends may emit
   * any unique-enough string; "" / absent means the backend doesn't
   * mint trace ids. */
  trace_id?: string;
}
