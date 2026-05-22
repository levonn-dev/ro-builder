import { JSDOM } from "jsdom";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { ScoreValidationError } from "../../errors.ts";
import type {
  BasePlus,
  CombatResults,
  DerivedStats,
  EnemyStats,
  EquipSpec,
  HardSoft,
  Level,
  MinMax,
  ShimSession,
  SkillAlloc,
  SlotKey,
  Stats,
} from "../../types.ts";

const __dirname = dirname(fileURLToPath(import.meta.url));
// __dirname is calc-sidecar/src/backends/rocalc; sidecar root is three up.
// vendor lives outside src/ so users can drop files there without touching
// our code, and the form-template (rocalc-shaped synthetic HTML) is a
// sibling of this file because it's part of the rocalc backend.
const SIDECAR_ROOT = resolve(__dirname, "../../..");
const VENDOR = resolve(SIDECAR_ROOT, "vendor/rocalc");
const FORM_TEMPLATE = resolve(__dirname, "form-template.html");
const MAPPING_FILE = resolve(__dirname, "mapping.json");

interface MappingEntry {
  iRO_id: number;
  rocalc_id: number;
  name: string;
}

interface MappingFile {
  items: MappingEntry[];
  cards: MappingEntry[];
  mobs?: MappingEntry[];
}

interface Mapping {
  items: Map<number, number>;
  cards: Map<number, number>;
  mobs: Map<number, number>;
}

// CLASS_TO_ROCALC_ID maps shim-contract class names to rocalc's m_Job
// indexing. The contract layer (callers, types.ts) speaks names; the
// translation to integer ids is rocalc's internal concern.
//
// Indices are verified against rocalc's m_JobBuff (skill list per class)
// and JobName arrays. The dropdown population code is
// `c.A_JOB.options[i] = new Option(JobName[i], i)`; i.e. JobName[i] is
// the canonical display name for m_Job index i.
//
// All 46 pre-renewal slots verifiably load via ClickJob under jsdom
// EXCEPT id 34 (High Novice; rocalc has an `[0]` placeholder for it
// with no real skill data, fails on missing m_Skill[0][1]). The
// `_preInsertValidity` patch in tolerateStaleParentChecks fixed the
// previously-broken trans-class IDs (37-39) by silencing JSDOM's strict
// removeChild check when an option's parent is OPTGROUP rather than the
// SELECT.
//
// Anything outside this table; and not parseable as a numeric id;
// throws "unknown class". Adding a class is a paired change: verify it
// loads via test/class-coverage.ts, then add the entry here.
const CLASS_TO_ROCALC_ID: Record<string, number> = {
  // First-class (1st job)
  novice: 0,
  swordman: 1,
  swordsman: 1, // common misspelling
  thief: 2,
  acolyte: 3,
  archer: 4,
  magician: 5,
  mage: 5, // alias
  merchant: 6,

  // Second-class (2nd job)
  knight: 7,
  assassin: 8,
  priest: 9,
  hunter: 10,
  wizard: 11,
  blacksmith: 12,
  crusader: 13,
  rogue: 14,
  monk: 15,
  bard: 16,
  dancer: 17,
  sage: 18,
  alchemist: 19,

  // Specials
  super_novice: 20,
  supernovice: 20, // alias

  // Trans (rebirth / second advancement)
  lord_knight: 21,
  assassin_cross: 22,
  sin_x: 22, // alias
  high_priest: 23,
  sniper: 24,
  high_wizard: 25,
  whitesmith: 26,
  paladin: 27,
  stalker: 28,
  champion: 29,
  clown: 30,
  minstrel: 30, // alias
  gypsy: 31,
  professor: 32,
  scholar: 32, // alias
  creator: 33,
  biochemist: 33, // alias

  // High first-class (post-rebirth, pre-2nd); id 34 (High Novice) is a
  // rocalc placeholder with empty skill data; it throws under setClass
  // and is intentionally absent.
  high_swordman: 35,
  high_swordsman: 35, // alias
  high_thief: 36,
  high_acolyte: 37,
  high_archer: 38,
  high_magician: 39,
  high_mage: 39, // alias
  high_merchant: 40,

  // Taekwon path
  taekwon: 41,
  taekwon_kid: 41, // alias for the current focus class
  star_gladiator: 42,
  taekwon_master: 42, // alias
  soul_linker: 43,

  // Expanded (pre-renewal expansion classes)
  ninja: 44,
  gunslinger: 45,
};

function normalizeClassName(s: string): string {
  return s
    .toLowerCase()
    .trim()
    .replace(/[\s-]+/g, "_");
}

// resolveRocalcClassID maps a class name (or numeric-string fallback) to
// rocalc's m_Job index. Throws an "unknown class" error with the valid
// names list when the lookup misses; server.ts classifies "unknown class"
// substrings as 4xx so callers see a helpful response.
function resolveRocalcClassID(className: string): number {
  const trimmed = className.trim();
  if (trimmed === "") return 0; // Novice; shim default
  // Numeric-string escape hatch for callers who know the rocalc id
  // directly (debugging, exotic classes not yet named in the table).
  if (/^[0-9]+$/.test(trimmed)) {
    const id = parseInt(trimmed, 10);
    if (id < 0)
      throw new ScoreValidationError(
        `unknown class id ${trimmed}; must be non-negative`,
      );
    return id;
  }
  const id = CLASS_TO_ROCALC_ID[normalizeClassName(trimmed)];
  if (id === undefined) {
    const valid = Object.keys(CLASS_TO_ROCALC_ID).sort().join(", ");
    throw new ScoreValidationError(
      `unknown class "${className}"; valid names: ${valid}`,
    );
  }
  return id;
}

// loadMapping reads cmd/build-rocalc-mapping's output and builds three
// iRO_id → rocalc_id Maps; items (weapon/armor/etc), cards, and mobs.
// rocalc keeps separate id spaces for each (m_Item, m_Card, m_Monster) and
// numeric values often collide across spaces, so the lookups must stay
// distinct. Read once at module load; the mapping is static for a given
// rocalc data snapshot.
function loadMapping(): Mapping {
  const raw = JSON.parse(readFileSync(MAPPING_FILE, "utf-8")) as MappingFile;
  const items = new Map<number, number>();
  const cards = new Map<number, number>();
  const mobs = new Map<number, number>();
  for (const e of raw.items) items.set(e.iRO_id, e.rocalc_id);
  for (const e of raw.cards) cards.set(e.iRO_id, e.rocalc_id);
  // `mobs` is optional in older mapping.json snapshots; tolerate its
  // absence so a stale checkout still loads, just with setEnemy() unable
  // to translate. Re-running the mapping tool repopulates it.
  for (const e of raw.mobs ?? []) mobs.set(e.iRO_id, e.rocalc_id);
  return { items, cards, mobs };
}

const MAPPING = loadMapping();

type IDSpace = "item" | "card" | "mob";

// translate looks up an iRO id in the appropriate ID space and throws a
// caller-friendly error if it isn't there. The mapping is incomplete by
// design (see cmd/build-rocalc-mapping/unmatched-*.txt for the gaps);
// callers asking for an unmapped iRO id should hit this rather than
// silently mis-equipping.
function translate(iroID: number, space: IDSpace): number {
  let map: Map<number, number>;
  switch (space) {
    case "item":
      map = MAPPING.items;
      break;
    case "card":
      map = MAPPING.cards;
      break;
    case "mob":
      map = MAPPING.mobs;
      break;
  }
  const rocalcID = map.get(iroID);
  if (rocalcID === undefined) {
    throw new ScoreValidationError(
      `iRO ${space} id ${iroID} has no rocalc mapping; add an override in ` +
        `cmd/build-rocalc-mapping/manual_overrides.json and re-run the generator`,
    );
  }
  return rocalcID;
}

// rocalc's load order, mirroring vendor/rocalc/index.html.
const ROCALC_FILES = [
  "skill_2026-04-06.js",
  "head_2026-04-06.js",
  "item_2026-04-06.js",
  "etc_2026-04-06.js",
  "monster_2026-04-06.js",
  "card_2026-04-06.js",
  "foot_2026-04-06.js",
];

// CALC_VERSION stamps every ScoreResponse so the Go build library can
// detect when a saved trajectory was scored under an older calc snapshot.
// Format: "rocalc-<YYYY-MM-DD>"; the date pulled from the vendored
// rocalc filenames above (they all share the same suffix per snapshot).
export const CALC_VERSION: string = (() => {
  const match = ROCALC_FILES[0].match(/(\d{4}-\d{2}-\d{2})/);
  return match ? `rocalc-${match[1]}` : "rocalc-unknown";
})();

interface SlotDef {
  item: string;
  refine: string | null;
  cardslot: number;
  weaponType?: boolean;
  shieldWeight?: boolean;
  cardFields: string[];
  cardHandler: "Click_Card" | "Card";
}

// Equipment-slot map. Keys are slot names exposed to callers. Per slot:
//   item:        form field for the item id
//   refine:      form field for the refinement value (null if non-refinable;
//                mid/bottom headgear, accessories)
//   cardslot:    rocalc's restrictCardslot index for filtering card options
//   weaponType:  weapon-only; pre-call ClickWeaponType in equip()
//   shieldWeight: shield-only; post-call setShieldWeight in equip()
//   cardFields:  ordered list of card-slot form fields. Weapon has up to 4,
//                most other slots have 1, head bottom has 0.
//   cardHandler: 'Click_Card' for weapon cards, 'Card' for everything else;
//                matches rocalc's per-card-select onchange handler.
const SLOTS: Record<SlotKey, SlotDef> = {
  weapon: {
    item: "A_weapon1",
    refine: "A_Weapon_refine",
    cardslot: 1,
    weaponType: true,
    cardFields: [
      "A_weapon1_card1",
      "A_weapon1_card2",
      "A_weapon1_card3",
      "A_weapon1_card4",
    ],
    cardHandler: "Click_Card",
  },
  headTop: {
    item: "A_head1",
    refine: "A_HEAD_REFINE",
    cardslot: 2,
    cardFields: ["A_head1_card"],
    cardHandler: "Card",
  },
  headMid: {
    item: "A_head2",
    refine: null,
    cardslot: 3,
    cardFields: ["A_head2_card"],
    cardHandler: "Card",
  },
  headBot: {
    item: "A_head3",
    refine: null,
    cardslot: 4,
    cardFields: [],
    cardHandler: "Card",
  },
  shield: {
    item: "A_left",
    refine: "A_LEFT_REFINE",
    cardslot: 5,
    shieldWeight: true,
    cardFields: ["A_left_card"],
    cardHandler: "Card",
  },
  armor: {
    item: "A_body",
    refine: "A_BODY_REFINE",
    cardslot: 6,
    cardFields: ["A_body_card"],
    cardHandler: "Card",
  },
  garment: {
    item: "A_shoulder",
    refine: "A_SHOULDER_REFINE",
    cardslot: 7,
    cardFields: ["A_shoulder_card"],
    cardHandler: "Card",
  },
  footgear: {
    item: "A_shoes",
    refine: "A_SHOES_REFINE",
    cardslot: 8,
    cardFields: ["A_shoes_card"],
    cardHandler: "Card",
  },
  accessory1: {
    item: "A_acces1",
    refine: null,
    cardslot: 9,
    cardFields: ["A_acces1_card"],
    cardHandler: "Card",
  },
  accessory2: {
    item: "A_acces2",
    refine: null,
    cardslot: 10,
    cardFields: ["A_acces2_card"],
    cardHandler: "Card",
  },
};

// rocalc's globals (Lang, PvP, ClickJob, StAllCalc, calc, etc.) get
// installed onto the jsdom window when we eval its scripts. TypeScript
// can't know about them, so we type the rocalc-bearing window as
// `RocalcWindow` (any) and the form as `RocalcForm` (intersection of the
// HTMLFormElement DOM type plus arbitrary string-indexed accessors that
// resolve to elements via the live-lookup proxy installed by
// installFormNameProxies). Internal jsdom plumbing in this file uses
// these aliases freely; the public surface stays tightly typed via
// ShimSession.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type RocalcWindow = any;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type RocalcForm = any;

interface FormSnapshot {
  [key: string]: { value: string; checked: boolean };
}

// createShim spins up a fresh jsdom window with rocalc loaded and returns a
// session object. Each call re-loads everything (~250 ms), so callers should
// reuse the returned session across operations rather than recreating it.
//
// Three jsdom-vs-rocalc compat steps applied here:
//   1. url is set so localStorage is exposed (rocalc touches it during init).
//   2. document.calcForm is set explicitly; jsdom doesn't expose the legacy
//      document.formName named-element shortcut.
//   3. form.X named/id access is implemented as a live-lookup getter via
//      Object.defineProperty + form.elements.namedItem. Caching the elements
//      directly (form[name] = el) breaks across class changes: ClickJob
//      rebuilds option lists, after which jsdom's removeChild rejects stale
//      cached refs ("child can not be found in the parent") even though
//      .parentNode looks correct. Live lookups stay valid.
export function createShim(): ShimSession {
  const html = readFileSync(FORM_TEMPLATE, "utf-8");
  const dom = new JSDOM(html, {
    runScripts: "outside-only",
    url: "http://localhost/",
  });
  const win = dom.window as unknown as RocalcWindow;
  const doc = win.document;

  win.eval("var Lang = 0; var PvP = 0;");

  const form: RocalcForm = doc.forms.calcForm;
  doc.calcForm = form;
  installFormNameProxies(form);
  tolerateStaleParentChecks(win);

  for (const file of ROCALC_FILES) {
    win.eval(readFileSync(resolve(VENDOR, file), "utf-8"));
  }

  // Backfill the per-class skill-buff arrays IFF rocalc didn't allocate
  // them. ClickJob references n_A_BufN[i] = ... for several N (no
  // number, 2, 3, 6, 7, 8, 9). Some branches allocate the array; some
  // don't; failure mode is "Cannot set properties of undefined" when a
  // class hits the assign without the alloc. The guard preserves any
  // allocation rocalc did itself; we only fill genuine undefined gaps
  // so the indexed assigns don't blow up. Calc semantics are
  // unchanged: reads use `1 * arr[i]` which is NaN→0 for empty slots.
  win.eval(`
		if (typeof n_A_Buf === 'undefined') n_A_Buf = [];
		if (typeof n_A_Buf2 === 'undefined') n_A_Buf2 = [];
		if (typeof n_A_Buf3 === 'undefined') n_A_Buf3 = [];
		if (typeof n_A_Buf6 === 'undefined') n_A_Buf6 = [];
		if (typeof n_A_Buf7 === 'undefined') n_A_Buf7 = [];
		if (typeof n_A_Buf8 === 'undefined') n_A_Buf8 = [];
		if (typeof n_A_Buf9 === 'undefined') n_A_Buf9 = [];
	`);

  // Disable rocalc's "auto-adjust BaseLV" UX. With it on (the form default),
  // setting a stat too high to fit the current level's status point pool
  // silently raises BaseLV until it fits. Programmatic callers want explicit
  // level control: the orchestrator decides the build's level, not the calc.
  // A_STPOINT going negative after setStats is the over-budget signal and is
  // surfaced via readDerivedStats().statPointsRemaining for caller inspection.
  form.BLVauto.checked = false;

  // foot.js's auto-init on script load only partially populates equipment
  // <select> option lists; A_weapon1 fills in but A_body/A_head1/A_shoes/etc
  // stay empty until ClickJob runs. Calling setClass(0) (Novice; the default
  // class) explicitly during init populates the full set so equip() works
  // without forcing every caller to setClass before equipping.
  win.ClickJob("0");
  win.StAllCalc();
  win.StCalc();

  // Snapshot post-init form state so reset() can restore it. Rocalc's globals
  // (n_A_JOB etc.) are derived from form values on StAllCalc, so restoring
  // form values + recomputing returns the session to the post-init baseline
  // for that class. setClass mutates these globals AND repopulates option
  // lists, so it refreshes the snapshot too; reset() after setClass rolls
  // back to the new class's baseline, not the original Novice one.
  let initialFormState = snapshotForm(form);

  function setStats({ str, agi, vit, int: int_, dex, luk }: Stats): void {
    form.A_STR.value = String(str);
    form.A_AGI.value = String(agi);
    form.A_VIT.value = String(vit);
    form.A_INT.value = String(int_);
    form.A_DEX.value = String(dex);
    form.A_LUK.value = String(luk);
    // Same trigger pair the form fires on a stat-select onChange.
    win.StCalc();
    win.StAllCalc();
  }

  function setLevel({ base, job }: Level): void {
    form.A_BaseLV.value = String(base);
    form.A_JobLV.value = String(job);
    // BaseLV onChange fires StCalc(1) | StAllCalc | WeaponSet2(1); JobLV is
    // just StAllCalc. Calling the former pair covers both.
    win.StCalc(1);
    win.StAllCalc();
    win.WeaponSet2(1);
  }

  // setClass switches the active class. Taekwon Kid is the current focus
  // class; the shim supports all 45 pre-renewal classes (id 34 High Novice
  // excluded; rocalc has placeholder data there). Names are translated to
  // rocalc's m_Job index via CLASS_TO_ROCALC_ID. ClickJob does a UI
  // rebuild that pokes at form elements; the form-template's
  // `<div id="clickjob-stubs">` block plus the JSDOM-internal
  // `_preInsertValidity` patch in tolerateStaleParentChecks together
  // cover the cases JSDOM rejects more strictly than a real browser.
  function setClass(className: string): void {
    const jobId = resolveRocalcClassID(className);
    form.A_JOB.value = String(jobId);
    win.ClickJob(String(jobId));
    win.StAllCalc();
    win.StCalc();
    win.restrictCardslot(0);
    // Refresh the reset() snapshot to this class's post-ClickJob baseline.
    // Without this, reset() rolls form values back to Novice defaults but
    // leaves n_A_JOB / option lists at the new class; inconsistent state
    // that StAllCalc reads incorrectly on the next mutation.
    initialFormState = snapshotForm(form);
  }

  // equip sets one equipment slot. id and cards are iRO IDs; the canonical
  // ID space every RO server emulator exposes (Knife=1201, Cotton Shirt=2301,
  // Andre Card=4043, etc); the shim translates them into rocalc's internal
  // indices via the mapping table loaded at module init. refine is 0-10 and
  // only valid for slots that accept it. cards is an array of iRO card IDs,
  // one per card-slot on the item, padded with 0s where unused; length must
  // not exceed the slot's max card count.
  function equip(
    slot: SlotKey,
    { id, refine = 0, cards = [] }: EquipSpec,
  ): void {
    const def = SLOTS[slot];
    if (!def) throw new ScoreValidationError(`unknown equipment slot: ${slot}`);
    if (refine !== 0 && !def.refine) {
      throw new ScoreValidationError(
        `slot "${slot}" does not support refinement`,
      );
    }
    if (cards.length > def.cardFields.length) {
      throw new ScoreValidationError(
        `slot "${slot}" supports max ${def.cardFields.length} card(s), got ${cards.length}`,
      );
    }
    const rocalcID = String(translate(id, "item"));
    // Verify the rocalc id is in the slot's current option list. The list
    // was populated by ClickJob for the active class, so an id not in it
    // means this class can't equip this item. Without this check rocalc's
    // ClickWeaponType / equivalent functions choke on m_Item[''] further
    // downstream with cryptic "Cannot read properties of undefined" errors.
    const select = form[def.item];
    let inOptions = false;
    for (let i = 0; i < select.options.length; i++) {
      if (select.options[i].value === rocalcID) {
        inOptions = true;
        break;
      }
    }
    if (!inOptions) {
      throw new ScoreValidationError(
        `item iRO ${id} (rocalc ${rocalcID}) not in current class's '${slot}' option list; class restriction or item not loaded`,
      );
    }
    select.value = rocalcID;
    if (def.refine) form[def.refine].value = String(refine);
    // Same onchange chain rocalc fires, in the same order. Weapon-only:
    // ClickWeaponType first, so StAllCalc sees the resolved weapon type.
    if (def.weaponType) win.ClickWeaponType(rocalcID);
    win.StAllCalc();
    win.ClickB_Item(rocalcID);
    win.restrictCardslot(def.cardslot);
    if (def.shieldWeight) win.setShieldWeight(rocalcID);

    // Cards are inserted after the item is equipped so the card-slot
    // <select>s have been re-populated by restrictCardslot. Clear any
    // existing cards first so equip is idempotent; a re-equip on the same
    // slot replaces the prior cards rather than ORing into them.
    for (const f of def.cardFields) form[f].value = "0";
    for (let i = 0; i < cards.length; i++) {
      const rocalcCardID = String(translate(cards[i], "card"));
      form[def.cardFields[i]].value = rocalcCardID;
      win[def.cardHandler](rocalcCardID);
    }
    if (cards.length > 0) win.StAllCalc();
  }

  // setEnemy switches the combat-simulator target. iroMobId is an iRO mob
  // id (Poring=1002, Eddga=1115); the shim translates to rocalc's
  // m_Monster index via the mobs section of mapping.json. Mirrors
  // rocalc's B_Enemy onchange chain: Bskill() rebuilds the per-class
  // skill panel for that enemy, then calc() refreshes the damage / hit /
  // battleTime output cells.
  //
  // Two layers of validation:
  //   1. translate() throws "no rocalc mapping" if iroMobId isn't in the
  //      mob mapping (155 mobs are unmapped as of the current snapshot;
  //      mostly bracket-variants like "Goblin [Axe]"; see
  //      cmd/build-rocalc-mapping/unmatched-mobs.txt).
  //   2. The post-translation bounds check guards against a mapping row
  //      with a rocalc id that's out of m_Monster's range (data
  //      corruption, hand-edited mapping). rocalc's internals (Bskill /
  //      calc / EnemySort) read m_Monster[mobId] without bounds checks
  //      of their own; an invalid id explodes deep inside rocalc with
  //      messages like "Cannot read properties of undefined" that don't
  //      tell the caller what they got wrong.
  //
  // Both error message patterns are recognized by server.ts's
  // classifyError as 4xx-able.
  // rocalc m_Monster row layout, discovered via test/m_monster_probe.ts:
  //   [0]  rocalc internal id
  //   [1]  display name
  //   [2]  race code (RACE_CODES below)
  //   [3]  element packed: code * 10 + level (1..4)
  //   [4]  size code (0=Small, 1=Medium, 2=Large)
  //   [5]  level
  //   [6]  HP
  //   [7-11] mob stats (str/agi/vit?/int?/dex/luk; order partially uncertain
  //          beyond Dex@10 / Luk@11; not load-bearing for inline scoring)
  //   [12] atk_min
  //   [13] atk_max
  //   [14] def (hard def; the calc applies pre-re soft-def from VIT separately)
  //   [15] mdef
  //   [19] boss flag (0=normal, non-zero=boss/mvp)
  //   [16-22, 23+] exp / drops / unrelated metadata; not read by combat code
  const RACE_CODES: Record<string, number> = {
    RC_Formless: 0,
    RC_Undead: 1,
    RC_Brute: 2,
    RC_Plant: 3,
    RC_Insect: 4,
    RC_Fish: 5,
    RC_Demon: 6,
    RC_DemiHuman: 7,
    RC_Angel: 8,
    RC_Dragon: 9,
  };
  const ELEMENT_CODES: Record<string, number> = {
    Ele_Neutral: 0,
    Ele_Water: 1,
    Ele_Earth: 2,
    Ele_Fire: 3,
    Ele_Wind: 4,
    Ele_Poison: 5,
    Ele_Holy: 6,
    Ele_Dark: 7,
    Ele_Ghost: 8,
    Ele_Undead: 9,
  };
  const SIZE_CODES: Record<string, number> = {
    Size_Small: 0,
    Size_Medium: 1,
    Size_Large: 2,
  };

  // Scratch slot for inline-stats scoring. We overwrite an existing
  // m_Monster row instead of appending; appending would require also
  // growing the B_Enemy <select>'s option list and any parallel arrays
  // rocalc indexes alongside m_Monster (drop tables, etc.), which the
  // probe revealed Bskill silently depends on. Overwriting Poring (272)
  // is safe because (a) the orchestrator keeps the active profile-aware
  // catalog separate from rocalc internals; score_build calls don't
  // share state across requests; (b) we always restore the row before
  // the function returns so concurrent stock-mob calls in the same shim
  // session keep seeing the canonical Poring stats. The pre-existing
  // "create new shim per request" pattern in score.ts gives us
  // per-request isolation regardless.
  const SCRATCH_SLOT = 272; // Poring's rocalc id

  function setEnemyInline(stats: EnemyStats): void {
    const monsters = win.m_Monster;
    if (!monsters || !Array.isArray(monsters)) {
      throw new Error(
        `m_Monster table not loaded; rocalc init may have failed`,
      );
    }
    // Snapshot the pre-call m_Monster row FIRST, before any of the
    // validation throws or the row assignment below. The finally-block
    // restores from `original`, so if anything between here and the
    // finally throws after we've started mutating, the restore still
    // returns m_Monster[SCRATCH_SLOT] to its true pre-call state.
    // (Previously this slice() ran AFTER `monsters[SCRATCH_SLOT] = row`,
    // which would have captured the inline row as "original" and
    // permanently leaked into Poring's slot.)
    const original = (monsters[SCRATCH_SLOT] as unknown[]).slice();
    // Strict lookup: unknown keys must NOT silently default. Defaulting
    // race→0/element→0/size→1 silently turns any unrecognized code into
    // Formless/Neutral1/Medium and the calc returns numbers that look
    // plausible but score against the wrong mob. Throw instead so the
    // caller (or its config) gets a 4xx pointing at the bad code.
    const raceCode = RACE_CODES[stats.race];
    if (raceCode === undefined) {
      throw new ScoreValidationError(
        `unknown enemy race code ${JSON.stringify(stats.race)}; expected one of: ${Object.keys(RACE_CODES).join(", ")}`,
      );
    }
    const elementCode = ELEMENT_CODES[stats.element];
    if (elementCode === undefined) {
      throw new ScoreValidationError(
        `unknown enemy element code ${JSON.stringify(stats.element)}; expected one of: ${Object.keys(ELEMENT_CODES).join(", ")}`,
      );
    }
    const elementLv = Math.max(1, Math.min(4, stats.element_lv || 1));
    const elementPacked = elementCode * 10 + elementLv;
    const sizeCode = SIZE_CODES[stats.size];
    if (sizeCode === undefined) {
      throw new ScoreValidationError(
        `unknown enemy size code ${JSON.stringify(stats.size)}; expected one of: ${Object.keys(SIZE_CODES).join(", ")}`,
      );
    }

    const row: unknown[] = [...original];
    row[2] = raceCode;
    row[3] = elementPacked;
    row[4] = sizeCode;
    // Mob level drives pre-renewal hit/flee scaling in the combat sim.
    // Default to 1 for backwards compat with callers that don't supply
    // it; otherwise pass through the caller's value verbatim. UARO's
    // OGH overlay (lvl 95-100) was previously hard-coded to 1 here,
    // which overestimated the player's hit rate by 10-20pp.
    const mobLevel = stats.level && stats.level > 0 ? stats.level : 1;
    row[5] = mobLevel;
    row[6] = stats.hp;
    row[12] = stats.atk_min;
    row[13] = stats.atk_max;
    row[14] = stats.def;
    row[15] = stats.mdef;
    monsters[SCRATCH_SLOT] = row;

    const originalEnemy = form.B_Enemy.value;
    try {
      form.B_Enemy.value = String(SCRATCH_SLOT);
      win.Bskill();
      win.calc();
    } finally {
      // Restore-before-read invariant: this finally restores
      // m_Monster[SCRATCH_SLOT] BEFORE score.ts calls readCombatResults().
      // It's safe today because rocalc's Bskill()/calc() above wrote the
      // combat outputs into <td> cells (strID_0..2, BattleTime, B_6, B_type,
      // etc.) while m_Monster still held our inline stats; readCombatResults
      // pulls from those DOM cells (see function above), not from the live
      // m_Monster array. Any future addition to readCombatResults that reads
      // m_Monster directly will silently get the restored Poring stats here;
      // either widen this try-block to cover the read or capture the needed
      // values into locals before the finally fires.
      monsters[SCRATCH_SLOT] = original;
      // Restore B_Enemy so a subsequent setClass → snapshotForm doesn't
      // bake the scratch slot into the new class's reset baseline.
      form.B_Enemy.value = originalEnemy;
    }
  }

  function setEnemy(iroMobId: number): void {
    if (
      typeof iroMobId !== "number" ||
      !Number.isInteger(iroMobId) ||
      iroMobId < 0
    ) {
      throw new ScoreValidationError(
        `mob id ${iroMobId} out of range; must be a non-negative integer`,
      );
    }
    const rocalcMobId = translate(iroMobId, "mob");

    const monsters = win.m_Monster;
    if (!monsters || !Array.isArray(monsters)) {
      throw new Error(
        `m_Monster table not loaded; rocalc init may have failed`,
      );
    }
    if (rocalcMobId >= monsters.length || monsters[rocalcMobId] == null) {
      throw new ScoreValidationError(
        `mob id ${iroMobId} (rocalc ${rocalcMobId}) out of range; m_Monster has ${monsters.length} entries; ` +
          `mapping.json appears stale, re-run cmd/build-rocalc-mapping`,
      );
    }
    form.B_Enemy.value = String(rocalcMobId);
    win.Bskill();
    win.calc();
  }

  // readCombatResults pulls the combat-sim output cells rocalc populates
  // after each calc() pass. Several cells render sentinel strings instead
  // of numbers when the build's stats fall outside what rocalc can model
  // (e.g. strID_2 becomes "Infinite (no 100% hit)" when hit rate is
  // sub-100, BattleTime becomes "Too high to calculate" when the kill
  // would exceed rocalc's iteration cap). Those sentinels surface here as
  // null so the orchestrator can branch on "can't compute" without parsing
  // free-form text.
  //
  // Cell-name trap: rocalc's "MinATKnum/AveATKnum/MaxATKnum" cells are
  // "Number of Hits to Kill", NOT per-hit damage. The actual damage cells
  // are strID_0/1/2 (rendered as "Minimum/Average/Maximum Damage" rows in
  // index.html). readMaybeInt's parseInt picks up the leading number, so
  // dual-wield decompositions like "95 (95 + 0)" still parse to the total.
  function readCombatResults(): CombatResults {
    return {
      damage: {
        min: readMaybeInt(doc, "strID_0"),
        ave: readMaybeInt(doc, "strID_1"),
        max: readMaybeInt(doc, "strID_2"),
        secondAve: readMaybeFloat(doc, "AveSecondATK"),
      },
      crit: {
        damage: readMaybeInt(doc, "CRIATK"),
        rate: parsePctSuffix(readCell(doc, "CRInum")),
      },
      hit: readMaybeFloat(doc, "BattleHIT"),
      dodge: readMaybeFloat(doc, "BattleFLEE"),
      battleTimeSec: parseSeconds(readCell(doc, "BattleTime")),
      incoming: {
        min: readMaybeInt(doc, "B_MinAtk"),
        ave: readMaybeInt(doc, "B_AveAtk"),
        max: readMaybeInt(doc, "B_MaxAtk"),
        aveWithDodge: readMaybeFloat(doc, "B_Ave2Atk"),
      },
      enemy: {
        hp: readMaybeInt(doc, "B_6"),
        race: readCell(doc, "B_2"),
        element: readCell(doc, "B_3"),
        size: readCell(doc, "B_4"),
        type: readCell(doc, "B_type"),
      },
    };
  }

  function readDerivedStats(): DerivedStats {
    return {
      hit: readInt(doc, "A_HIT"),
      flee: readInt(doc, "A_FLEE"),
      cri: readInt(doc, "A_CRI"),
      atk: readBasePlus(doc, "A_ATK2"),
      matk: readMinMax(doc, "A_MATK"),
      def: readHardSoft(doc, "A_totalDEF"),
      mdef: readHardSoft(doc, "A_MDEF"),
      aspd: readFloat(doc, "A_ASPD"),
      maxHp: readInt(doc, "A_MaxHP"),
      maxSp: readInt(doc, "A_MaxSP"),
      statPointsRemaining: readInt(doc, "A_STPOINT"),
    };
  }

  function reset(): void {
    restoreForm(form, initialFormState);
    win.StAllCalc();
    win.StCalc();
    // calc() refreshes the combat-sim DOM cells (strID_0..2, BattleTime,
    // B_MinAtk, etc.). Without this, those cells retain values from
    // the prior request's setEnemy/setEnemyInline. The current score()
    // flow only reads them after a fresh setEnemy* call so this is
    // defense-in-depth; it keeps reset()'s contract ("session is back
    // at baseline") true for every cell, not just form fields.
    win.calc();
  }

  // setSkills assigns levels to the active class's skill slots. Each
  // entry's id is an iRO skill id; rocalc's m_JobBuff[classId] is a list
  // of iRO skill ids the class can learn (skill IDs are Gravity-canonical,
  // so iRO and rocalc agree without a translation table). The slot index
  // in m_JobBuff[classId] matches the form's A_skill0..A_skill21 selects.
  //
  // Validation policy:
  //  - empty array is a no-op.
  //  - **id not in m_JobBuff[classId] is silently skipped.** rocalc's
  //    m_JobBuff is the calc engine's stat-effect-skill set, not the
  //    player's allocatable skill tree (skill_tree.conf). Many skills
  //    the player legitimately allocates (TK kicks, Knight active
  //    damage skills) aren't modeled by rocalc as stat-effect entries,
  //    but they should pass through scoring without 400-ing the
  //    request. Calc returns auto-attack-tier numbers for such
  //    snapshots; that's a known limitation, not an error.
  //  - level outside [0, max] is clamped to the legal range before
  //    assignment. JSDOM accepts arbitrary <select>.value writes (even
  //    values not in the option list), and rocalc's StAllCalc reads
  //    `1 * select.value` directly; passing level=99 on a max-10 skill
  //    would apply 99-level bonuses, well past anything the player
  //    could legitimately reach in-game.
  function setSkills(skills: SkillAlloc[]): void {
    if (skills.length === 0) return;
    const classId = parseInt(form.A_JOB.value, 10);
    const jobBuff = win.m_JobBuff?.[classId];
    if (!Array.isArray(jobBuff)) {
      throw new Error(
        `m_JobBuff[${classId}] missing; class skill list not loaded by ClickJob`,
      );
    }
    for (const { id, level } of skills) {
      const slotIndex = jobBuff.indexOf(id);
      if (slotIndex < 0) {
        // Skip silently; see method doc.
        continue;
      }
      const select = form[`A_skill${slotIndex}`];
      if (!select) {
        throw new Error(
          `form has no A_skill${slotIndex}; skill slot index out of form range`,
        );
      }
      // Clamp to the select's actual option range. m_Skill[id][1] is the
      // skill's max level per rocalc's schema; the select's options are
      // populated to match (values "0".."max"). Walking select.options
      // for the largest numeric value works even when the schema lookup
      // would be too tightly coupled to rocalc internals. Fall back to 10
      // (no pre-renewal skill exceeds 10) when the option list is empty
      // for any reason.
      let maxLevel = 0;
      for (let i = 0; i < select.options.length; i++) {
        const v = parseInt(select.options[i].value, 10);
        if (Number.isFinite(v) && v > maxLevel) maxLevel = v;
      }
      if (maxLevel === 0) maxLevel = 10;
      const clamped = Math.max(0, Math.min(maxLevel, level));
      select.value = String(clamped);
    }
    // Same recompute pair the form fires on a skill onChange.
    win.StAllCalc();
    win.StCalc();
  }

  const session: ShimSession = {
    setStats,
    setLevel,
    setClass,
    equip,
    setSkills,
    setEnemy,
    setEnemyInline,
    readDerivedStats,
    readCombatResults,
    reset,
  };
  return session;
}

// snapshotForm captures every named/id'd form control's value and checked
// state. The pair is captured for every element regardless of type so reset()
// can restore both without per-element type dispatch.
function snapshotForm(form: RocalcForm): FormSnapshot {
  const state: FormSnapshot = {};
  for (const el of form.elements) {
    const key = el.name || el.id;
    if (!key || key in state) continue;
    state[key] = { value: el.value, checked: el.checked };
  }
  return state;
}

function restoreForm(form: RocalcForm, state: FormSnapshot): void {
  for (const [key, snap] of Object.entries(state)) {
    const el = form.elements.namedItem(key);
    if (!el) continue;
    el.value = snap.value;
    el.checked = snap.checked;
  }
}

// installFormNameProxies projects every named/id'd form control onto the form
// object as a live-lookup getter. See createShim's comment for rationale.
function installFormNameProxies(form: RocalcForm): void {
  const projected = new Set<string>();
  for (const el of form.elements) {
    for (const key of [el.name, el.id]) {
      if (!key || projected.has(key)) continue;
      projected.add(key);
      Object.defineProperty(form, key, {
        configurable: true,
        enumerable: true,
        get() {
          return form.elements.namedItem(key);
        },
        // No-op setter: rocalc never reassigns c.X = element, only c.X.value.
        // Defining a setter (rather than leaving it undefined) keeps assignments
        // silent instead of throwing in strict mode.
        set() {},
      });
    }
  }
}

// Patch JSDOM's NodeImpl `_preInsertValidity` and `_replace` to swallow the
// "child can not be found in the parent" check. These two checks fire when
// rocalc tries to insert/replace into a select where the existing option's
// parent is an OPTGROUP rather than the SELECT itself; a strict-spec
// rejection that a real browser permits in practice. The end-state DOM is
// fine; the throw is the only thing blocking ClickJob from finishing.
//
// We have to walk the impl prototype chain because the validity check lives
// on the JSDOM-internal `NodeImpl` class, accessed through each public DOM
// element's Symbol(impl) reference. Public Node.prototype patches don't
// reach this path; JSDOM calls `_impl._preInsertValidity` directly.
//
// The JSDOM NodeImpl prototype is shared across all JSDOM instances in a
// process, so this patch only needs to run once. _patchApplied guards
// against accumulating wrapper closures across many createShim() calls.
let _patchApplied = false;

function tolerateStaleParentChecks(win: RocalcWindow): void {
  if (_patchApplied) return;
  const probe = win.document.createElement("select");
  const implSym = Object.getOwnPropertySymbols(probe).find(
    (s) => s.toString() === "Symbol(impl)",
  );
  if (!implSym) return;
  let proto = Object.getPrototypeOf(probe[implSym]);
  while (proto && proto.constructor.name !== "Object") {
    const names = Object.getOwnPropertyNames(proto);
    if (names.includes("_preInsertValidity") && names.includes("_replace")) {
      break;
    }
    proto = Object.getPrototypeOf(proto);
  }
  if (!proto) return;

  for (const method of ["_preInsertValidity", "_replace"]) {
    const orig = proto[method];
    if (typeof orig !== "function") continue;
    proto[method] = function (this: unknown, ...args: unknown[]): unknown {
      try {
        return orig.apply(this, args);
      } catch (err) {
        const msg = (err as Error)?.message ?? "";
        if (msg.includes("child can not be found in the parent")) {
          return undefined;
        }
        throw err;
      }
    };
  }
  _patchApplied = true;
}

// Cell readers; rocalc writes derived stats into <td id="A_X"> elements
// rather than form fields. Most are plain integers; a few are composite
// strings ("base+plus", "min~max", "hard+soft") that we split here so callers
// see structured numbers rather than display strings.

function readCell(doc: Document, id: string): string {
  const el = doc.getElementById(id);
  if (!el) throw new Error(`output cell #${id} missing from form-template`);
  return (el.textContent ?? "").trim();
}

function readInt(doc: Document, id: string): number {
  const text = readCell(doc, id);
  const n = parseInt(text, 10);
  if (Number.isNaN(n))
    throw new Error(`#${id} not an integer: ${JSON.stringify(text)}`);
  return n;
}

function readFloat(doc: Document, id: string): number {
  const text = readCell(doc, id);
  const n = parseFloat(text);
  if (Number.isNaN(n))
    throw new Error(`#${id} not a number: ${JSON.stringify(text)}`);
  return n;
}

function readBasePlus(doc: Document, id: string): BasePlus {
  const [base, plus] = splitOn(readCell(doc, id), "+", id);
  return { base, plus };
}

function readMinMax(doc: Document, id: string): MinMax {
  const [min, max] = splitOn(readCell(doc, id), "~", id);
  return { min, max };
}

function readHardSoft(doc: Document, id: string): HardSoft {
  const [hard, soft] = splitOn(readCell(doc, id), "+", id);
  return { hard, soft };
}

// readMaybeInt parses an integer cell, returning null when the cell holds a
// non-numeric sentinel like "Infinite (no 100% hit)" or "Over 10000 hits".
// Distinct from readInt which throws on parse failure; that's the right
// behavior for derived-stat cells that always render numbers, but combat-sim
// cells legitimately render sentinels for unsolvable builds.
function readMaybeInt(doc: Document, id: string): number | null {
  const text = readCell(doc, id);
  const n = parseInt(text, 10);
  return Number.isNaN(n) ? null : n;
}

function readMaybeFloat(doc: Document, id: string): number | null {
  const text = readCell(doc, id);
  const n = parseFloat(text);
  return Number.isNaN(n) ? null : n;
}

// parsePctSuffix extracts the numeric portion from cells like " (0%)" or
// "15.93%"; strips parens, percent sign, and surrounding whitespace.
function parsePctSuffix(text: string): number | null {
  if (!text) return null;
  const m = text.match(/(-?\d+(?:\.\d+)?)\s*%/);
  if (!m) return null;
  const n = parseFloat(m[1]);
  return Number.isNaN(n) ? null : n;
}

// parseSeconds extracts the leading number from BattleTime cells like
// "62.55 seconds". Returns null for "Too high to calculate" and similar.
function parseSeconds(text: string): number | null {
  if (!text) return null;
  const n = parseFloat(text);
  return Number.isNaN(n) ? null : n;
}

function splitOn(text: string, sep: string, id: string): [number, number] {
  const parts = text.split(sep);
  if (parts.length !== 2)
    throw new Error(`#${id} not a "${sep}"-pair: ${JSON.stringify(text)}`);
  const a = parseInt(parts[0], 10);
  const b = parseInt(parts[1], 10);
  if (Number.isNaN(a) || Number.isNaN(b)) {
    throw new Error(
      `#${id} not numeric pair on "${sep}": ${JSON.stringify(text)}`,
    );
  }
  return [a, b];
}
