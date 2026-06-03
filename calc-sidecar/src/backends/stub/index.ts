// Stub calc backend: deterministic in-memory state, no I/O.
//
// Numbers returned here are FICTION. Tests that assert numeric
// correctness against this backend are mis-written; the stub exists
// to exercise the shim contract and HTTP plumbing without rocalc's
// vendor files on disk. Active when CALC_BACKEND=stub
// (see ../registry.ts).

import type {
  Buff,
  CombatResults,
  DerivedStats,
  EnemyStats,
  EquipSpec,
  Level,
  ShimSession,
  SkillAlloc,
  SlotKey,
  Stats,
} from "../../types.ts";

export const CALC_VERSION = "stub-v1";

const STUB_DERIVED: DerivedStats = {
  hit: 100,
  flee: 0,
  cri: 0,
  atk: { base: 100, plus: 0 },
  matk: { min: 0, max: 0 },
  def: { hard: 0, soft: 0 },
  mdef: { hard: 0, soft: 0 },
  aspd: 150,
  maxHp: 1000,
  maxSp: 100,
  statPointsRemaining: 0,
};

const STUB_COMBAT: CombatResults = {
  damage: { min: 100, ave: 100, max: 100, secondAve: null },
  crit: { damage: 0, rate: 0 },
  hit: 100,
  dodge: 0,
  battleTimeSec: 1,
  incoming: { min: 0, ave: 0, max: 0, aveWithDodge: 0 },
  enemy: {
    hp: 1000,
    race: "Demi-Human",
    element: "Neutral",
    size: "Medium",
    type: "Normal",
  },
};

interface StubState {
  className: string;
  level: Level;
  stats: Stats;
  equipment: Partial<Record<SlotKey, EquipSpec>>;
  skills: SkillAlloc[];
  enemy: number | null;
  enemyInline: EnemyStats | null;
}

function freshState(): StubState {
  return {
    className: "novice",
    level: { base: 1, job: 1 },
    stats: { str: 1, agi: 1, vit: 1, int: 1, dex: 1, luk: 1 },
    equipment: {},
    skills: [],
    enemy: null,
    enemyInline: null,
  };
}

export function createShim(): ShimSession {
  let state = freshState();

  return {
    setClass(name) {
      state.className = name;
    },
    setLevel(level) {
      state.level = level;
    },
    setStats(stats) {
      state.stats = stats;
    },
    equip(slot, spec) {
      state.equipment[slot] = spec;
    },
    setSkills(skills) {
      state.skills = skills;
    },
    setBuffs(_buffs: Buff[]): void {
      // stub: deterministic fiction, buffs don't alter output
    },
    setEnemy(id) {
      state.enemy = id;
      state.enemyInline = null;
    },
    setEnemyInline(s) {
      state.enemyInline = s;
      state.enemy = null;
    },
    readDerivedStats() {
      return structuredClone(STUB_DERIVED);
    },
    readCombatResults() {
      return structuredClone(STUB_COMBAT);
    },
    reset() {
      state = freshState();
    },
  };
}
