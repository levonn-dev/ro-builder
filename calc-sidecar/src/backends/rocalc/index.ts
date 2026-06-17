import { JSDOM } from "jsdom";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { ScoreValidationError } from "../../errors.ts";
import type {
  BasePlus,
  Buff,
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
  // installBuffControls must run BEFORE installFormNameProxies so the
  // A2_Skill*/A5_Skill*/B_debuf* controls are in form.elements when the
  // proxy getters are installed. The proxies use form.elements.namedItem(key)
  // as a live lookup, so they remain valid even after the DOM under a given
  // name changes (e.g. ClickJob rebuilding option lists). We also need the
  // controls present before rocalc loads so rocalc's own init path (which
  // accesses c.A2_Skill* via these proxies) doesn't throw on missing elements.
  installBuffControls(doc);
  installFormNameProxies(form);
  tolerateStaleParentChecks(win);

  for (const file of ROCALC_FILES) {
    win.eval(readFileSync(resolve(VENDOR, file), "utf-8"));
  }

  // foot.js's init block (top-level code at the end of the file) calls
  // refreshFields() which calls BufSW(n_SkillSW). Since n_SkillSW=0 at
  // that point, BufSW takes the else branch and rebuilds #SIENSKILL with
  // just a collapsed header row -- wiping our installBuffControls-injected
  // A2_Skill* controls. Reinstall them now so the live proxy getters can
  // resolve the controls again. installBuffControls removes any existing
  // controls by name before injecting, so calling it twice is idempotent.
  installBuffControls(doc);

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

  // pendingDebufActions stores the enemy_debuf actions from the most recent
  // setBuffs call so setEnemyInline/setEnemy can re-apply them after Bskill.
  // Background: StAllCalc() (called at the end of setBuffs) triggers
  // ClickB_Enemy() -> debufSW(n_debufSW), which rebuilds #EnemyDebuf with
  // fresh empty controls and re-reads n_B_debuf[] from form. If the active
  // enemy at that point is NOT undead/demon, debufSW explicitly zeros
  // n_B_debuf[12] (Signum) and disables the control, discarding the value we
  // just set. Re-applying the form values AFTER the enemy scratch slot is
  // written but BEFORE calc() -- and calling ClickB_Enemy() again with the
  // actual target in place -- lets debufSW run with the correct enemy context
  // so race-gated debuffs are not zeroed. Lex Aeterna (B_debuf6, checkbox)
  // survives the first round-trip because debufSW writes n_B_debuf[6] back
  // to c.B_debuf6.checked without a race gate, so the round-trip is lossless
  // for checkboxes; selects (B_debuf11, B_debuf12) need this second pass.
  type PendingDebuf = {
    field: string;
    control: "select" | "checkbox";
    level: number;
    forceEnable: boolean;
  };
  let pendingDebufActions: PendingDebuf[] = [];

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
      // Re-apply race-gated enemy debuffs (Signum Crucis, Decrease AGI) now
      // that the actual target is in the scratch slot. setBuffs's StAllCalc
      // ran ClickB_Enemy with the default enemy (not undead/demon), causing
      // debufSW to zero n_B_debuf[12] for non-qualifying targets. Now that
      // the undead/demon target is in place, reapplyPendingDebufs re-writes
      // the form controls and calls ClickB_Enemy() again so debufSW reads
      // the correct race/element and doesn't zero them.
      reapplyPendingDebufs();
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
    // Re-apply race-gated enemy debuffs with the actual target in place.
    // Same rationale as setEnemyInline; see reapplyPendingDebufs comment.
    reapplyPendingDebufs();
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
    // Clear pending debuf actions so a subsequent setClass/setEnemy* does not
    // re-apply stale debuffs from a prior request.
    pendingDebufActions = [];
    // Zero the section-gate globals. restoreForm() only restores form field
    // values; n_SkillSW and n_debufSW are rocalc JS globals that setBuffs
    // set to 1. If they stay at 1, the next setClass() triggers ClickJob ->
    // StAllCalc -> ClickB_Enemy -> debufSW(1) / BufSW(1), which runs the
    // full panel-rebuild path (BufSW uses myInnerHtml to build #SIENSKILL,
    // debufSW uses myInnerHtml to build #EnemyDebuf). Those myInnerHtml calls
    // can hit elements that don't exist under the current DOM state, causing
    // "Cannot read properties of null" errors. Zeroing here makes the
    // subsequent setClass trigger the safe collapsed-header paths (BufSW(0) /
    // debufSW(0)) instead.
    win.n_SkillSW = 0;
    win.n_debufSW = 0;
    win.n_Skill6SW = 0;
    // Zero the n_B_debuf[] array directly. ClickB_Enemy only re-reads from
    // form into n_B_debuf[] when n_debufSW is truthy; with n_debufSW = 0,
    // stale values (e.g. n_B_debuf[6]=1 from Lex Aeterna) persist in memory
    // and still influence the damage formula in calc() even without n_debufSW.
    // Zeroing explicitly ensures the next calc() sees a clean debuff state.
    if (Array.isArray(win.n_B_debuf)) {
      for (let i = 0; i < win.n_B_debuf.length; i++) win.n_B_debuf[i] = 0;
    }
    // Zero n_A_Buf2[] (support skill effects). StAllCalc reads n_A_Buf2 into
    // derived stat calculations ONLY when n_SkillSW is truthy (see foot.js:
    // `n_SkillSW && (n_A_Buf2[0]=1*c.A2_Skill0.value, ...)`). With
    // n_SkillSW = 0, stale n_A_Buf2 values (blessing = STR/DEX bonus, etc.)
    // persist in memory and still influence the stat calculations. Zeroing
    // directly ensures the post-reset StAllCalc sees no support-buff residue.
    if (Array.isArray(win.n_A_Buf2)) {
      for (let i = 0; i < win.n_A_Buf2.length; i++) win.n_A_Buf2[i] = 0;
    }
    // Zero n_A_Buf6[] (land bank). calc() reads the land effect from n_A_Buf6[]
    // WITHOUT re-checking n_Skill6SW (e.g. `0==n_A_Buf6[0] && n_A_Buf6[1]>=1`),
    // so a stale Volcano/Deluge/Violent Gale level would leak into the next
    // request even with the gate off. Same vector as n_B_debuf[].
    if (Array.isArray(win.n_A_Buf6)) {
      for (let i = 0; i < win.n_A_Buf6.length; i++) win.n_A_Buf6[i] = 0;
    }
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

  // BUFF_BINDINGS maps a semantic buff name to ordered rocalc actions. Each
  // action drives an existing control. skill_slot writes an m_JobBuff A_skill
  // slot (rocalc's internal skill id, indexOf'd into m_JobBuff[classId]);
  // weapon_endow forces the A_Weapon_element select; support_slot writes one
  // of the A2_Skill* player-support controls (n_SkillSW section); enemy_debuf
  // writes a B_debuf* enemy-debuff control (n_debufSW section).
  // This is the only place rocalc's internal ids live; the contract stays
  // semantic. Extend by adding a binding + (for a new control surface) a
  // driver case below.
  type BuffAction =
    | { driver: "skill_slot"; rocalcId: number; level: number | "buffLevel" }
    | { driver: "weapon_endow" }
    | { driver: "support_slot"; field: string; control: "select" | "checkbox" }
    | {
        driver: "enemy_debuf";
        field: string;
        control: "select" | "checkbox";
        forceEnable?: boolean;
      }
    | { driver: "land_buff"; landType: 0 | 1 | 2 };
  const BUFF_BINDINGS: Record<string, BuffAction[]> = {
    // Taekwon (existing)
    taekwon_ranker: [{ driver: "skill_slot", rocalcId: 345, level: 1 }],
    spurt: [
      { driver: "skill_slot", rocalcId: 379, level: 1 }, // Spurt status (+STR/ATK); m_JobBuff[41] index 4
      { driver: "skill_slot", rocalcId: 329, level: "buffLevel" }, // Sprint unarmed mastery; m_JobBuff[41] index 3; drives combat damage (not ATK), verified barehanded TK lv7 adds ~94 ave damage vs neutral mob
    ],
    mild_wind: [{ driver: "weapon_endow" }],
    // High Priest player support buffs (A2_Skill* bank)
    blessing: [
      { driver: "support_slot", field: "A2_Skill0", control: "select" },
    ],
    increase_agi: [
      { driver: "support_slot", field: "A2_Skill1", control: "select" },
    ],
    impositio_manus: [
      { driver: "support_slot", field: "A2_Skill2", control: "select" },
    ],
    gloria: [
      { driver: "support_slot", field: "A2_Skill3", control: "checkbox" },
    ],
    angelus: [
      { driver: "support_slot", field: "A2_Skill4", control: "select" },
    ],
    assumptio: [
      { driver: "support_slot", field: "A2_Skill5", control: "checkbox" },
    ],
    suffragium: [
      { driver: "support_slot", field: "A2_Skill10", control: "select" },
    ],
    aspersio: [{ driver: "weapon_endow" }],
    // High Priest enemy debuffs (B_debuf* bank)
    lex_aeterna: [
      { driver: "enemy_debuf", field: "B_debuf6", control: "checkbox" },
    ],
    decrease_agi: [
      { driver: "enemy_debuf", field: "B_debuf11", control: "select" },
    ],
    signum_crucis: [
      {
        driver: "enemy_debuf",
        field: "B_debuf12",
        control: "select",
        forceEnable: true,
      },
    ],
    // Scholar (Professor / Sage) weapon endows -- existing weapon_endow driver.
    // Element comes from the resolved buff.element (overlay endow.elements).
    flame_launcher: [{ driver: "weapon_endow" }],
    frost_weapon: [{ driver: "weapon_endow" }],
    lightning_loader: [{ driver: "weapon_endow" }],
    seismic_weapon: [{ driver: "weapon_endow" }],
    // Scholar enemy debuffs -- existing enemy_debuf driver.
    mind_breaker: [
      { driver: "enemy_debuf", field: "B_debuf18", control: "select" },
    ],
    spider_web: [
      { driver: "enemy_debuf", field: "B_debuf17", control: "checkbox" },
    ],
    // Scholar A_skill passives -- existing skill_slot driver (rocalc internal ids).
    energy_coat: [{ driver: "skill_slot", rocalcId: 58, level: "buffLevel" }],
    study: [{ driver: "skill_slot", rocalcId: 224, level: "buffLevel" }],
    dragonology: [{ driver: "skill_slot", rocalcId: 234, level: "buffLevel" }],
    foresight: [{ driver: "skill_slot", rocalcId: 322, level: "buffLevel" }], // iRO PF_MEMORIZE
    double_casting: [
      { driver: "skill_slot", rocalcId: 441, level: "buffLevel" },
    ], // iRO PF_DOUBLECASTING
    // Scholar land buffs -- new land_buff driver (A6_Skill0 land type + A6_Skill1 level).
    volcano: [{ driver: "land_buff", landType: 0 }],
    deluge: [{ driver: "land_buff", landType: 1 }],
    violent_gale: [{ driver: "land_buff", landType: 2 }],
    // Assassin Cross (m_JobBuff[22] = [537,13,14,79,80,81,262,266,381]; base
    // Assassin row 8 = [537,13,14,79,80,81,381]). rocalc ids diverge from Aegis
    // ids, so allocation alone drops these passives; drive them explicitly.
    enchant_deadly_poison: [
      { driver: "skill_slot", rocalcId: 266, level: "buffLevel" },
    ],
    enchant_poison: [{ driver: "weapon_endow" }],
    advanced_katar_mastery: [
      { driver: "skill_slot", rocalcId: 262, level: "buffLevel" },
    ],
    katar_mastery: [{ driver: "skill_slot", rocalcId: 81, level: "buffLevel" }],
    right_hand_mastery: [
      { driver: "skill_slot", rocalcId: 79, level: "buffLevel" },
    ],
    left_hand_mastery: [
      { driver: "skill_slot", rocalcId: 80, level: "buffLevel" },
    ],
    double_attack: [{ driver: "skill_slot", rocalcId: 13, level: "buffLevel" }],
    improve_dodge: [{ driver: "skill_slot", rocalcId: 14, level: "buffLevel" }],
    sonic_acceleration: [
      { driver: "skill_slot", rocalcId: 381, level: "buffLevel" },
    ],
    // Sniper (m_JobBuff[24] = [537,38,39,42,116,118,119,270,273,390]; base Hunter
    // row 10 = [537,38,39,42,116,118,119,390]). rocalc ids diverge from Aegis ids,
    // so allocation alone drops these; drive them explicitly. Owl's / Vulture's
    // scale +1/level (up to +10) -- buffLevel carries the allocated level.
    owls_eye: [{ driver: "skill_slot", rocalcId: 38, level: "buffLevel" }],
    vultures_eye: [{ driver: "skill_slot", rocalcId: 39, level: "buffLevel" }],
    improve_concentration: [
      { driver: "skill_slot", rocalcId: 42, level: "buffLevel" },
    ],
    beast_bane: [{ driver: "skill_slot", rocalcId: 116, level: "buffLevel" }],
    steel_crow: [{ driver: "skill_slot", rocalcId: 119, level: "buffLevel" }],
    true_sight: [{ driver: "skill_slot", rocalcId: 270, level: "buffLevel" }],
    wind_walk: [{ driver: "skill_slot", rocalcId: 273, level: "buffLevel" }],
  };

  // A_Weapon_element option values (empirically probed; value 0 = "(unchanged)"
  // i.e. leave the weapon's native element, there is no force-neutral option).
  const ENDOW_ELEMENT_VALUES: Record<string, string> = {
    neutral: "0",
    water: "1",
    earth: "2",
    fire: "3",
    wind: "4",
    poison: "5",
    holy: "6",
    shadow: "7",
    ghost: "8",
    undead: "9",
  };
  // MILD_WIND_ORDER mirrors the endow.elements list in
  // internal/catalog/data/skill_buffs.yaml (TK_SEVENWIND entry).
  // That YAML list is the SOURCE OF TRUTH; this const is a defense-in-depth
  // copy because the sidecar cannot read the catalog at runtime. Keep the two
  // in sync: any reorder in the YAML must be reflected here, and the
  // behavioral pinning tests in test/backends/rocalc/buffs.test.ts assert the
  // boundary conditions so a divergence fails a test.
  //
  // Element index (1-based) == required Mild Wind level: earth=1..holy=7.
  const MILD_WIND_ORDER = [
    "earth",
    "wind",
    "water",
    "fire",
    "ghost",
    "shadow",
    "holy",
  ];

  // Returns true if the slot was found and written, false if this class's
  // m_JobBuff has no entry for rocalcId (the engine doesn't model this skill
  // for the active class). setBuffs uses the return to tell a buff that was
  // fully applied from one that is entirely inapplicable to the class.
  function applySkillSlot(rocalcId: number, level: number): boolean {
    const classId = parseInt(form.A_JOB.value, 10);
    const jobBuff = win.m_JobBuff?.[classId];
    if (!Array.isArray(jobBuff)) {
      throw new Error(
        `m_JobBuff[${classId}] missing; class buff list not loaded by ClickJob`,
      );
    }
    const slotIndex = jobBuff.indexOf(rocalcId);
    if (slotIndex < 0) {
      // Buff's rocalc id not modeled for this class; report unapplied so the
      // caller can decide whether the buff applied at all (mirrors setSkills'
      // silent skip per action, but surfaces the miss to setBuffs).
      return false;
    }
    const select = form[`A_skill${slotIndex}`];
    if (!select) {
      throw new Error(
        `form has no A_skill${slotIndex}; buff slot index out of form range`,
      );
    }
    let maxLevel = 0;
    for (let i = 0; i < select.options.length; i++) {
      const v = parseInt(select.options[i].value, 10);
      if (Number.isFinite(v) && v > maxLevel) maxLevel = v;
    }
    if (maxLevel === 0) maxLevel = 10;
    select.value = String(Math.max(0, Math.min(maxLevel, level)));
    return true;
  }

  // setSelectClamped writes a level to a <select>, clamped to its largest
  // numeric option (so an over-allocated level can't apply past the control's
  // real max). Shared by the support_slot and enemy_debuf drivers.
  function setSelectClamped(select: RocalcForm, level: number): void {
    let maxLevel = 0;
    for (let i = 0; i < select.options.length; i++) {
      const v = parseInt(select.options[i].value, 10);
      if (Number.isFinite(v) && v > maxLevel) maxLevel = v;
    }
    if (maxLevel === 0) maxLevel = 10;
    select.value = String(Math.max(0, Math.min(maxLevel, level)));
  }

  // applyEnemyDebuf drives one EnemyDebuf control (B_debuf* bank). The control
  // always exists (installBuffControls). forceEnable clears the `disabled`
  // attribute rocalc sets on race-gated debuffs (Signum is disabled off-target);
  // rocalc's combat code still applies the effect only when the target qualifies
  // (e.g. Signum's DEF cut only vs undead/demon), so force-enabling is safe and
  // faithful. The section gate n_debufSW is set by setBuffs once any enemy_debuf
  // action ran; values round-trip through n_B_debuf[] and survive the later
  // setEnemy* rebuild (score.ts runs setBuffs first).
  function applyEnemyDebuf(
    field: string,
    control: "select" | "checkbox",
    level: number,
    forceEnable: boolean,
  ): void {
    const el = form[field];
    if (!el) {
      throw new Error(
        `enemy-debuff control ${field} missing; installBuffControls not run?`,
      );
    }
    if (forceEnable) el.disabled = false;
    if (control === "checkbox") {
      el.checked = level > 0;
    } else {
      setSelectClamped(el, level);
    }
    // Record for re-application after setEnemy*/Bskill. rocalc's debufSW()
    // (called from ClickB_Enemy -> StAllCalc inside setBuffs) rebuilds
    // #EnemyDebuf with empty controls and zeros race-gated debuffs (Signum
    // Crucis, Decrease AGI) when the default enemy is not undead/demon.
    // setEnemyInline/setEnemy re-apply these after the actual target is in
    // place, then call ClickB_Enemy() so n_B_debuf[] picks up the correct
    // values with the right race/element context.
    pendingDebufActions.push({ field, control, level, forceEnable });
  }

  // reapplyPendingDebufs re-asserts enemy_debuf form values after the target
  // enemy slot has been set (scratch or stock). Called inside setEnemyInline /
  // setEnemy after Bskill() and before calc(). rocalc's debufSW() (called by
  // ClickB_Enemy inside StAllCalc during setBuffs) zeros race-gated select
  // debuffs (Signum, Decrease AGI) when the default enemy is not the right
  // race/element. By re-writing the form controls and calling ClickB_Enemy()
  // with the actual target in place, we let debufSW run with the correct
  // n_B[2]/n_B[3] context so Signum is not zeroed for an undead target and
  // n_B_debuf[12] is correctly read into the combat calculation.
  // This is a no-op when there are no pending debuf actions (no setBuffs call
  // with an enemy_debuf buff, or after reset()).
  function reapplyPendingDebufs(): void {
    if (pendingDebufActions.length === 0) return;
    for (const { field, control, level, forceEnable } of pendingDebufActions) {
      const el = form[field];
      if (!el) continue; // debufSW rebuilt #EnemyDebuf; element lookup is live via proxy
      if (forceEnable) el.disabled = false;
      if (control === "checkbox") {
        el.checked = level > 0;
      } else {
        // debufSW just populated options 0..max for this select; clamping is
        // safe even though the select was empty before debufSW ran (during the
        // first StAllCalc in setBuffs). The options are now present because
        // ClickB_Enemy (called by the Bskill path above) already triggered
        // debufSW which populates them unconditionally.
        setSelectClamped(el, level);
      }
    }
    // Re-call ClickB_Enemy so rocalc re-reads n_B_debuf[] from the form
    // controls (including the values we just wrote) with the current enemy
    // context. debufSW runs again inside ClickB_Enemy; this time with the
    // actual target's n_B[2]/n_B[3] so race-gated selects are not zeroed.
    win.ClickB_Enemy();
  }

  // applySupportSlot drives one player "Supportive / Party Skills" control
  // (A2_Skill* bank). The control always exists (installBuffControls), so this
  // is unconditional; the section gate n_SkillSW is set by setBuffs once any
  // support action ran. checkbox -> on iff level>0; select -> clamped level.
  function applySupportSlot(
    field: string,
    control: "select" | "checkbox",
    level: number,
  ): void {
    const el = form[field];
    if (!el) {
      throw new Error(
        `support control ${field} missing; installBuffControls not run?`,
      );
    }
    if (control === "checkbox") {
      el.checked = level > 0;
      return;
    }
    setSelectClamped(el, level);
  }

  // applyLandBuff drives the land controls: A6_Skill0 = land type
  // (Volcano=0/Deluge=1/Violent Gale=2), A6_Skill1 = land level. Both controls
  // always exist (installBuffControls injects them into #SIENSKILL alongside the
  // support bank, not rocalc's native #SP_SIEN04, which holds pet controls we
  // keep). The section gate n_Skill6SW is set by setBuffs once any land action
  // ran. Last-wins on A6_Skill0/A6_Skill1 if two land buffs are declared (one
  // land at a time, like one weapon element).
  function applyLandBuff(landType: 0 | 1 | 2, level: number): void {
    const typeSel = form["A6_Skill0"];
    const lvlSel = form["A6_Skill1"];
    if (!typeSel || !lvlSel) {
      throw new Error(
        "land controls A6_Skill0/A6_Skill1 missing; installBuffControls not run?",
      );
    }
    typeSel.value = String(landType);
    setSelectClamped(lvlSel, level);
  }

  function applyWeaponEndow(buff: Buff): void {
    const element = buff.element ?? "";
    const value = ENDOW_ELEMENT_VALUES[element];
    if (value === undefined) {
      throw new ScoreValidationError(
        `unknown endow element ${JSON.stringify(element)}; expected one of: ${Object.keys(ENDOW_ELEMENT_VALUES).join(", ")}`,
      );
    }
    // Level-unlock check applies only to Mild Wind (earth=1..holy=7 by skill
    // level). Other weapon-endow buffs (Aspersio, etc.) have no level gating
    // and must not hit this check. A new buff with its own unlock table adds
    // its own guard here when the binding is added.
    if (buff.name === "mild_wind") {
      const order = MILD_WIND_ORDER.indexOf(element) + 1; // 1-based; 0 if not in list
      const level = buff.level ?? 0;
      if (order > 0 && order > level) {
        throw new ScoreValidationError(
          `endow element ${JSON.stringify(element)} (order ${order}) exceeds Mild Wind level ${level}`,
        );
      }
    }
    form.A_Weapon_element.value = value;
  }

  function setBuffs(buffs: Buff[]): void {
    if (buffs.length === 0) return;
    // Clear the pending-debuf list so a fresh setBuffs call always reflects
    // only the current request's enemy_debuf actions. This also clears it for
    // the unbuffed path (empty buffs skips the whole function, so the list
    // from any prior call is preserved intentionally -- reset() is the
    // canonical way to clear session state).
    pendingDebufActions = [];
    // Resilience: reject a buff repeated in one request. The Go resolver
    // rejects duplicates upstream, but the backend must not silently
    // last-write-wins (or double-apply) on a repeat that slips through.
    const seen = new Set<string>();
    for (const buff of buffs) {
      if (seen.has(buff.name)) {
        throw new ScoreValidationError(
          `duplicate buff ${JSON.stringify(buff.name)}; each buff may appear at most once`,
        );
      }
      seen.add(buff.name);
    }
    let usedSupport = false;
    let usedDebuf = false;
    let usedLand = false;
    for (const buff of buffs) {
      const actions = BUFF_BINDINGS[buff.name];
      if (!actions) {
        throw new ScoreValidationError(
          `unknown buff ${JSON.stringify(buff.name)}; expected one of: ${Object.keys(BUFF_BINDINGS).join(", ")}`,
        );
      }
      // Track whether the buff produced any effect. An endow always applies (or
      // throws); a skill_slot applies only if the class's m_JobBuff has the
      // slot. A buff that touches the engine in zero ways is inapplicable to
      // the active class: surface it rather than scoring unbuffed numbers as if
      // the buff were active.
      let applied = false;
      for (const action of actions) {
        switch (action.driver) {
          case "skill_slot": {
            const level =
              action.level === "buffLevel" ? (buff.level ?? 0) : action.level;
            if (applySkillSlot(action.rocalcId, level)) applied = true;
            break;
          }
          case "weapon_endow":
            applyWeaponEndow(buff);
            applied = true;
            break;
          case "support_slot":
            applySupportSlot(action.field, action.control, buff.level ?? 0);
            applied = true;
            usedSupport = true;
            break;
          case "enemy_debuf":
            applyEnemyDebuf(
              action.field,
              action.control,
              buff.level ?? 0,
              action.forceEnable ?? false,
            );
            applied = true;
            usedDebuf = true;
            break;
          case "land_buff":
            applyLandBuff(action.landType, buff.level ?? 0);
            applied = true;
            usedLand = true;
            break;
        }
      }
      if (!applied) {
        throw new ScoreValidationError(
          `buff ${JSON.stringify(buff.name)} is not applicable to the active class (no engine slot); the calc cannot model it for this class`,
        );
      }
    }
    // Enable the player-support section so StAllCalc reads the A2_Skill* bank.
    // Leaving it on across reset() is fine: reset() restores the controls to 0,
    // so a later unbuffed request reads zeros (no leak).
    if (usedSupport) win.n_SkillSW = 1;
    // Enable the enemy-debuff section so the combat calc reads the B_debuf* bank.
    // Same leak-free reasoning as usedSupport: reset() restores controls to 0.
    if (usedDebuf) win.n_debufSW = 1;
    // Enable the land section so calc() reads the A6_Skill bank into n_A_Buf6[].
    // Same leak-free reasoning: reset() restores the controls to 0 AND zeros
    // n_A_Buf6[] (the land effect reads it without re-checking the gate).
    if (usedLand) win.n_Skill6SW = 1;
    // StAllCalc + StCalc: the pair setSkills fires. calc(): additionally needed
    // when a weapon_endow action changed A_Weapon_element, since the element-vs-
    // enemy multiplier is computed in calc(), not StAllCalc.
    win.StAllCalc();
    win.StCalc();
    win.calc();
  }

  const session: ShimSession = {
    setStats,
    setLevel,
    setClass,
    equip,
    setSkills,
    setBuffs,
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

// installBuffControls populates the two stub tables rocalc leaves empty until
// its BufSW()/AI() builders run: #SIENSKILL (player "Supportive / Party Skills",
// A2_Skill*/A5_Skill*) and #EnemyDebuf (B_debuf*). We author them statically
// instead of calling rocalc's builders because those use myInnerHtml (replaces
// innerHTML, which would break the live-lookup form-name proxies). Only the
// controls each section's gated read references need to exist; rocalc reads all
// of them when n_SkillSW / n_debufSW is set (StAllCalc / the enemy calc). Driven
// selects get their real 0..max option ranges (so the driver's clamp matches
// rocalc); every other select gets a single "0" option and is read as 0. Runs
// once per shim, before installFormNameProxies, so the new controls get proxied.
function installBuffControls(doc: Document): void {
  // Player support bank: read range n_A_Buf2[0..21].
  const SUPPORT_SELECTS = [0, 1, 2, 4, 6, 9, 10, 11, 12, 13, 14, 15];
  const SUPPORT_CHECKS = [3, 5, 7, 8];
  // Enemy-debuff bank: read range n_B_debuf[0..24]; these indices are selects,
  // the rest checkboxes.
  const DEBUF_SELECTS = new Set([0, 1, 11, 12, 18, 23, 24]);
  // Land / ground bank: read range n_A_Buf6[]. The section gate n_Skill6SW makes
  // rocalc read every one of these (by name), so all must exist as form
  // controls. We drive only A6_Skill0 (land type: Volcano=0/Deluge=1/Violent
  // Gale=2) and A6_Skill1 (land level 0..5); the rest exist inert (default
  // 0/unchecked). Injected into #SIENSKILL alongside the support bank because
  // they only need to be form elements bound by the name proxies, not in their
  // native #SP_SIEN04 container (which holds pet/other controls we must keep).
  const LAND_SELECTS = [0, 1, 4, 5, 18];
  const LAND_CHECKS = [3, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17];
  // 0..max option range for the controls WE drive; everything else -> "0" only.
  const SELECT_MAX: Record<string, number> = {
    A2_Skill0: 10,
    A2_Skill1: 10,
    A2_Skill2: 5,
    A2_Skill4: 10,
    A2_Skill10: 3,
    B_debuf11: 10,
    B_debuf12: 10,
    B_debuf18: 5,
    A6_Skill0: 2,
    A6_Skill1: 5,
  };
  const options = (name: string): string => {
    const max = SELECT_MAX[name] ?? 0;
    let h = "";
    for (let i = 0; i <= max; i++) h += `<option value="${i}">${i}</option>`;
    return h;
  };
  const sel = (name: string): string =>
    `<select name="${name}" id="${name}">${options(name)}</select>`;
  const chk = (name: string): string =>
    `<input type="checkbox" name="${name}" id="${name}">`;

  let support = "";
  for (const i of SUPPORT_SELECTS) support += sel(`A2_Skill${i}`);
  for (const i of SUPPORT_CHECKS) support += chk(`A2_Skill${i}`);
  for (let i = 0; i <= 5; i++) support += chk(`A5_Skill${i}`);
  for (const i of LAND_SELECTS) support += sel(`A6_Skill${i}`);
  for (const i of LAND_CHECKS) support += chk(`A6_Skill${i}`);

  let debuf = "";
  for (let i = 0; i <= 24; i++) {
    debuf += DEBUF_SELECTS.has(i) ? sel(`B_debuf${i}`) : chk(`B_debuf${i}`);
  }

  const sien = doc.getElementById("SIENSKILL");
  const enemy = doc.getElementById("EnemyDebuf");
  if (!sien || !enemy) {
    throw new Error(
      "form-template missing #SIENSKILL or #EnemyDebuf container for buff controls",
    );
  }

  // The form-template's #clickjob-stubs div (which must keep its other ~312
  // controls) holds bare duplicates of these same A2_Skill/A5_Skill/B_debuf
  // names. Remove those duplicates first so the optioned controls we inject are
  // the single canonical control per name (form.elements.namedItem would
  // otherwise depend on document order to return ours, not the bare stub whose
  // only option is "0" -> every driven level would clamp to 0).
  const managed: string[] = [];
  for (const i of SUPPORT_SELECTS) managed.push(`A2_Skill${i}`);
  for (const i of SUPPORT_CHECKS) managed.push(`A2_Skill${i}`);
  for (let i = 0; i <= 5; i++) managed.push(`A5_Skill${i}`);
  for (let i = 0; i <= 24; i++) managed.push(`B_debuf${i}`);
  for (const i of LAND_SELECTS) managed.push(`A6_Skill${i}`);
  for (const i of LAND_CHECKS) managed.push(`A6_Skill${i}`);
  for (const name of managed) {
    for (const el of Array.from(doc.querySelectorAll(`[name="${name}"]`))) {
      el.remove();
    }
  }

  sien.innerHTML = `<tr><td>${support}</td></tr>`;
  enemy.innerHTML = `<tr><td>${debuf}</td></tr>`;
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
