import { describe, it, expect, vi } from "vitest";
import { cronFields, parseCron, toCron } from "./cron-mapping";

// The suite sweeps a corpus of tens of thousands of expressions per test, some
// round-tripping every one; that is well within a second locally but crosses
// the default 5s cap on a loaded CI runner. Give the whole-corpus sweeps room.
vi.setConfig({ testTimeout: 30_000 });

// Hand-written example lists are how corners get missed: they only ever cover
// the shapes someone thought of. This suite covers the grammar by construction
// instead — it enumerates every token form of every field, and judges the result
// against an independent reference implementation of robfig/cron v3's parser
// rather than against expected strings someone typed out.
//
// The reference is the oracle. If the editor and the reference disagree about
// what an expression means, the editor is wrong — that is the whole point.

// ---------------------------------------------------------------------------
// Reference implementation of robfig/cron v3 (5-field), from its source.
// ---------------------------------------------------------------------------

interface Bounds {
  min: number;
  max: number;
  names?: Record<string, number>;
}

const MONTH_NAMES = {
  jan: 1, feb: 2, mar: 3, apr: 4, may: 5, jun: 6,
  jul: 7, aug: 8, sep: 9, oct: 10, nov: 11, dec: 12,
};
const DOW_NAMES = { sun: 0, mon: 1, tue: 2, wed: 3, thu: 4, fri: 5, sat: 6 };

const MINUTE: Bounds = { min: 0, max: 59 };
const HOUR: Bounds = { min: 0, max: 23 };
const DOM: Bounds = { min: 1, max: 31 };
const MONTH: Bounds = { min: 1, max: 12, names: MONTH_NAMES };
const DOW: Bounds = { min: 0, max: 6, names: DOW_NAMES };

/** One parsed field: the values it selects, and whether it was written as a
 *  wildcard. robfig keeps that bit because dom/dow switch between AND and OR
 *  depending on it. */
interface Field {
  values: Set<number>;
  star: boolean;
}

class CronSyntaxError extends Error {}

function parseIntOrName(token: string, b: Bounds): number {
  const named = b.names?.[token.toLowerCase()];
  if (named !== undefined) return named;
  // robfig numbers go through strconv.Atoi, which takes leading zeros and a
  // plus sign: "009", "+9" and "MON-05" are all legal. (Overflow past int is an
  // Atoi error; here it parses to a huge number and fails the bounds check —
  // same rejection, different wording.)
  if (!/^\+?\d+$/.test(token)) throw new CronSyntaxError(`bad token ${token}`);
  return parseInt(token, 10);
}

/** robfig getRange: `<range>` or `<range>/<step>`, where `<range>` is `*`, `?`,
 *  `N`, or `N-M`. A bare `N` with a step means `N-max/step`. */
function getRange(expr: string, b: Bounds): Field {
  const parts = expr.split("/");
  if (parts.length > 2) throw new CronSyntaxError(`too many slashes in ${expr}`);
  const rangeStr = parts[0]!;
  if (rangeStr === "") throw new CronSyntaxError(`empty range in ${expr}`);

  let step = 1;
  if (parts.length === 2) {
    const stepStr = parts[1]!;
    // Steps are Atoi numbers too: "*/+5" is a step of 5 to the backend.
    if (!/^\+?\d+$/.test(stepStr)) throw new CronSyntaxError(`bad step ${stepStr}`);
    step = parseInt(stepStr, 10);
    if (step === 0) throw new CronSyntaxError("step of zero");
  }

  // The wildcard test is on the range's LOW END, not on the whole range
  // (parser.go:261), and the branch it takes never looks at what followed the
  // "-" — not to parse it, not even to count the hyphens. So "*-19", "?-5" and
  // "*-19-25" are all the full span, exactly as "*" is. Writing this as an
  // equality on the whole range instead is what a reference implementation gets
  // wrong by being tidier than the thing it stands in for: it then calls those
  // expressions syntax errors, and the suite below dutifully checks the editor
  // against a grammar the server does not have.
  const ends = rangeStr.split("-");
  const star = ends[0] === "*" || ends[0] === "?";
  let low: number;
  let high: number;
  if (star) {
    low = b.min;
    high = b.max;
  } else {
    if (ends.length > 2) throw new CronSyntaxError(`too many dashes in ${expr}`);
    low = parseIntOrName(ends[0]!, b);
    if (ends.length === 2) {
      high = parseIntOrName(ends[1]!, b);
    } else {
      // "N/step" means "N-max/step"; a bare "N" is just N.
      high = parts.length === 2 ? b.max : low;
    }
  }
  if (low < b.min || high > b.max) throw new CronSyntaxError(`${expr} out of bounds`);
  if (low > high) throw new CronSyntaxError(`${expr} runs backwards`);

  const values = new Set<number>();
  for (let v = low; v <= high; v += step) values.add(v);
  // A step wider than 1 CLEARS the star bit (parser.go:297-298, `if step > 1 {
  // extra = 0 }`). It is not a detail: the bit is what makes dom/dow AND rather
  // than OR, so in "0 9 */2 * 1" neither field is a wildcard and the schedule
  // fires on every even day-of-month OR every Monday — not on their intersection.
  return { values, star: star && step === 1 };
}

function parseField(expr: string, b: Bounds): Field {
  const values = new Set<number>();
  let star = false;
  // robfig splits lists with strings.FieldsFunc (parser.go:238), which DROPS
  // empty elements — so "0,30," and "1,,5" are accepted, and a field of nothing
  // but commas parses to a field that selects no value and therefore never
  // fires. A plain split() would call all of these syntax errors and make this
  // reference stricter than the parser it is standing in for.
  for (const part of expr.split(",").filter((p) => p.length > 0)) {
    const f = getRange(part, b);
    for (const v of f.values) values.add(v);
    if (f.star) star = true;
  }
  return { values, star };
}

interface Spec {
  minute: Field;
  hour: Field;
  dom: Field;
  month: Field;
  dow: Field;
  /** The `TZ=` / `CRON_TZ=` prefix's zone, null when the expression has none.
   *  An empty name is spelled "UTC", which is what LoadLocation("") loads. */
  tz: string | null;
  /** Whether LoadLocation would accept that zone. The FIELDS of an expression
   *  with a bad zone still parse — the server rejects the expression, but at
   *  the timezone, and the editor's extraction relies on the difference. */
  tzValid: boolean;
  /** The expression with the prefix stripped, trimmed: the five fields as the
   *  parser reads them, and as the editor's `raw` holds them. */
  body: string;
}

/** LoadLocation's verdict on every zone name the corpus uses. The oracle
 *  cannot load real timezones, and it must not ask the browser — Intl and Go
 *  disagree (Go loads "Local", Intl throws on it) — so the corpus keeps to
 *  names whose verdict is pinned here. */
const TZ_VERDICTS: Record<string, boolean> = {
  UTC: true,
  "Asia/Tokyo": true,
  "Asia/Shanghai": true,
  Local: true,
  "Bogus/Zone": false,
  "A=B": false,
  "UTC\t": false,
};

/** robfig's prefix read (parser.go Parse), exactly: case-sensitive, on the
 *  untrimmed text, name up to the FIRST LITERAL SPACE. No space at all is the
 *  v3.0.1 panic (parser.go:99), which the server's guard turns into a plain
 *  rejection — either way the expression is refused, so the oracle rejects. */
function referencePrefix(expr: string): { tz: string; tzValid: boolean; rest: string } | "reject" | null {
  if (!expr.startsWith("TZ=") && !expr.startsWith("CRON_TZ=")) return null;
  const space = expr.indexOf(" ");
  if (space === -1) return "reject";
  const name = expr.slice(expr.indexOf("=") + 1, space);
  const tzValid = TZ_VERDICTS[name === "" ? "UTC" : name];
  if (tzValid === undefined) throw new Error(`corpus uses unpinned timezone ${JSON.stringify(name)}`);
  return { tz: name === "" ? "UTC" : name, tzValid, rest: expr.slice(space).trim() };
}

/** Returns null when robfig would reject the expression's structure. A parsed
 *  Spec with tzValid=false is still a server rejection — of the zone, not of
 *  the fields; callers that mean "does the server accept this" must check both. */
function reference(expr: string): Spec | null {
  const prefix = referencePrefix(expr);
  if (prefix === "reject") return null;
  const body = prefix === null ? expr : prefix.rest;
  const parts = body.trim().split(/\s+/).filter((p) => p.length > 0);
  if (parts.length !== 5) return null;
  try {
    return {
      minute: parseField(parts[0]!, MINUTE),
      hour: parseField(parts[1]!, HOUR),
      dom: parseField(parts[2]!, DOM),
      month: parseField(parts[3]!, MONTH),
      dow: parseField(parts[4]!, DOW),
      tz: prefix === null ? null : prefix.tz,
      tzValid: prefix === null ? true : prefix.tzValid,
      body: body.trim(),
    };
  } catch (err) {
    if (err instanceof CronSyntaxError) return null;
    throw err;
  }
}

/** Does the server accept this expression as a whole — fields AND zone? */
function serverAccepts(spec: Spec | null): spec is Spec {
  return spec !== null && spec.tzValid;
}

/** Where the EDITOR's extraction applies. Syntactic — it also fires over a
 *  body the server rejects ("TZ=UTC not a cron" echoes as the pair
 *  ("not a cron", UTC), which the server rejects just the same) — plus one
 *  gate the syntax cannot see: the zone must be one the browser could have
 *  offered in the picker, since config.timezone is a picker value and a
 *  formatter argument. "Local" is legal to LoadLocation (the SERVER host's
 *  zone) but no browser zone at all, so it stays buried in the verbatim text,
 *  where it keeps meaning what it meant. Not where robfig's read would panic
 *  (no space) or would leave a second prefix as a field — there the editor
 *  must keep the text verbatim too, and this returns null. */
function extractedForEcho(expr: string): { tz: string; rest: string } | null {
  const p = referencePrefix(expr);
  if (p === null || p === "reject") return null;
  if (p.rest.length === 0) return null;
  if (p.rest.startsWith("TZ=") || p.rest.startsWith("CRON_TZ=")) return null;
  const canonical = canonicalPickerZone(p.tz);
  if (canonical === null) return null;
  return { tz: canonical, rest: p.rest };
}

/** The browser's verdict on a zone name — the same Intl probe the editor runs,
 *  since the editor's contract IS "what the picker can hold": the canonical
 *  spelling the picker's list uses, or null when the browser cannot hold it. */
function canonicalPickerZone(tz: string): string | null {
  try {
    return new Intl.DateTimeFormat(undefined, { timeZone: tz }).resolvedOptions().timeZone;
  } catch {
    return null;
  }
}

/** robfig dayMatches: dom and dow are ANDed while either is a wildcard, and
 *  ORed once both are restricted. Two specs are day-equivalent iff they agree on
 *  every (day-of-month, day-of-week) pair. */
function dayMatches(spec: Spec, dom: number, dow: number): boolean {
  const domHit = spec.dom.values.has(dom);
  const dowHit = spec.dow.values.has(dow);
  return spec.dom.star || spec.dow.star ? domHit && dowHit : domHit || dowHit;
}

function sameSet(a: Set<number>, b: Set<number>): boolean {
  return a.size === b.size && [...a].every((v) => b.has(v));
}

/** Do two expressions fire at exactly the same instants? Compared over the whole
 *  domain — every minute, hour, month, and every (dom, dow) pair — so this is an
 *  exact equivalence, not a sampled one. */
function sameSchedule(a: Spec, b: Spec): boolean {
  if (!sameSet(a.minute.values, b.minute.values)) return false;
  if (!sameSet(a.hour.values, b.hour.values)) return false;
  if (!sameSet(a.month.values, b.month.values)) return false;
  for (let dom = 1; dom <= 31; dom++) {
    for (let dow = 0; dow <= 6; dow++) {
      if (dayMatches(a, dom, dow) !== dayMatches(b, dom, dow)) return false;
    }
  }
  return true;
}

// ---------------------------------------------------------------------------
// The corpus: every token form of every field.
// ---------------------------------------------------------------------------

// Each list walks one field through the whole closed grammar — wildcard,
// question mark, bare values at both bounds and past them, names, ranges
// (normal, degenerate, reversed, out of bounds), steps (on a wildcard, on a
// range, anchored to a value, of one, of zero, wider than the field), lists,
// Atoi's lexical variance (leading zeros, plus signs, overflow), and syntax
// that is simply malformed.
//
// Every field also carries the wildcard-low-end forms ("*-19", "?-5", "*-19/2"),
// which robfig reads as the full span with the upper end unread. They are the
// corner the reference above used to get wrong, so they belong in every list: a
// grammar quirk that lives in getRange lives in all five fields at once.
const MINUTE_TOKENS = [
  "*", "?", "0", "5", "30", "59", "60", "-5",
  "*/1", "*/5", "*/59", "*/60", "*/0", "?/5",
  "0/5", "15/5", "59/5",
  "0-30", "0-30/10", "30-0", "0-59",
  "0,30", "0,15,30,45", "0-10,30", "0-30/10,45",
  // robfig drops empty list elements, so these are legal, not typos.
  "0,30,", ",0,30", "1,,5", ",", "0,", ",0", "30,",
  // A list with a step-1 wildcard part means "*"; one whose extra part does not
  // parse is the syntax error the server calls it, wildcard or no wildcard.
  "0,*", "*,*/2", "?,15", "0,*-30", "*,60", "*,abc",
  // Atoi numbers: leading zeros and plus signs are legal, overflow is not.
  "09", "009", "00", "+30", "030-045", "00/05", "*/05", "*/+5",
  "99999999999999999999", "+",
  // The upper end of a wildcard range is unread — nonsense there is not an error.
  "*-30", "?-30", "*-30/10", "?-30/10", "*-99", "*-abc", "*-10-20", "9-?",
  "5-5", "", "*/", "0//5", "0--30", "abc",
];

const HOUR_TOKENS = [
  "*", "?", "0", "9", "23", "24", "-1",
  "*/1", "*/2", "*/23", "*/24", "*/0",
  "0/2", "9/2", "23/2", "9/1", "?/2",
  "*-19", "?-19", "*-19/2", "?-19/2", "*-19/23", "9-19/23",
  "9-21", "9-21/2", "9-21/1", "21-9", "0-23", "0-23/1", "9-9",
  // A window one hour wide, stepped: the editor writes this itself when a
  // dragged start meets its end. Past the model's interval bound ("/24") it has
  // no window form left and must collapse to the hour it selects.
  "9-9/3", "9-9/24", "9-11/5", "9-21/23", "9-21/30",
  "9,12,15", "9-12,18", "0-23/6", "9,12,", "9,,12", "9,", ",9", "0,",
  "9,*", "*,*/2",
  "09", "021", "+9", "09-021/02",
  "", "*/", "9//2", "abc",
];

const DOM_TOKENS = [
  "*", "?", "1", "15", "31", "0", "32",
  "*/2", "?/2", "1/2", "1-15", "1-15/2", "15-1",
  "*-15", "?-15", "*-15/2", "?/40",
  "1,15", "1-7,15", "1,15,", "1,,15", "15,", ",15", "1,*", "*,32", "L", "15W",
  "015", "01", "+15",
  "", "abc",
];

const MONTH_TOKENS = [
  "*", "?", "1", "6", "12", "0", "13",
  "JAN", "jan", "DEC", "FOO",
  "*/2", "?/2", "1-6", "1-6/2", "1,6", "6-1", "1,6,", "1,,6", "6,", ",6",
  "1,*", "JAN,*", "FOO,*",
  "*-6", "?-6", "*-6/2", "JAN-JUN/2",
  "06", "012", "+6",
  "", "abc",
];

const DOW_TOKENS = [
  "*", "?", "0", "1", "6", "7", "-1",
  "SUN", "sun", "SAT", "MON", "FOO",
  "*/2", "*/7", "?/2", "1/2", "MON/2", "0/3",
  "*-5", "?-5", "*-5/2", "?-5/2", "SUN-SAT/2",
  // A step wider than the week. robfig has no upper bound on a step, so these are
  // legal and degenerate: they select the range's first day and nothing else
  // ("*/100" is Sunday, "1-5/60" and "MON-FRI/60" are Monday). Day-of-week was the
  // last field whose parser drew its own step bound and refused them.
  "*/100", "1-5/60", "MON-FRI/60",
  "1-5", "MON-FRI", "mon-fri", "sun-sat", "0-6", "5-1", "1-5/2", "1-1",
  "1,3,5", "MON,WED", "5,1,5", "1,3-5", "0-2,4",
  // Empty list elements are dropped by the backend, so these are Mon–Fri and
  // {Mon, Fri} — and dow is the one field whose echo domain is "anything
  // enumerable", so they have to come back as chips, not as advanced-only.
  "1-5,", ",1,5", "1,,5", ",", "1,", ",1", "5,",
  "1,*", "MON,*", "*/2,*/3", "*,7",
  "001", "01", "+1", "+0", "MON-05", "0-06/02",
  "1#2", "5L",
  "", "abc",
];

// The model is two dimensions — a time and a set of days — so crossing the cron
// FIELDS pairwise is not enough: it leaves the two dimensions themselves
// uncrossed. A first cut of this suite did exactly that (minute x hour over a
// wildcard day, dom x dow over a fixed time) and a mutant that dropped the
// day-of-month whenever the time was an interval survived every invariant. Each
// full field-cross below is therefore replayed against every branch of the other
// dimension.

/** One expression per branch of the day logic: unrestricted, each weekly shape,
 *  each day-of-month boundary, and the dom+dow pair cron ORs. */
const DAY_BRANCHES = ["* * *", "* * 1-5", "* * 0", "* * 6", "1 * *", "15 * *", "31 * *", "15 * 1"];

/** One expression per branch of the time logic: fixed, hourly and N-hourly (with
 *  and without a window and a minute offset), and the minute-step forms. */
const TIME_BRANCHES = [
  "0 9", "30 14", "0 *", "15 *", "0 */2", "30 */3",
  "0 9-21", "30 9-21", "0 9-21/2", "* *", "*/10 *", "*/10 9-18", "*/15 9",
];

/** Every expression the corpus produces. */
function corpus(): string[] {
  const out = new Set<string>();

  // minute x hour, fully crossed — the model carries a single step dimension, so
  // whether a minute step may sit beside an hour step is settled here — and the
  // whole cross replayed against every shape the day dimension can take.
  for (const m of MINUTE_TOKENS) {
    for (const h of HOUR_TOKENS) {
      for (const days of DAY_BRANCHES) out.add(`${m} ${h} ${days}`);
    }
  }

  // dom x dow, fully crossed — cron ORs these two once both are restricted,
  // which the orthogonal model cannot express — against every shape of time.
  for (const dom of DOM_TOKENS) {
    for (const dow of DOW_TOKENS) {
      for (const time of TIME_BRANCHES) out.add(`${time} ${dom} * ${dow}`);
    }
  }

  // month, on its own axis: it does not interact with anything, but it still has
  // to survive both dimensions.
  for (const mo of MONTH_TOKENS) {
    for (const time of TIME_BRANCHES) out.add(`${time} * ${mo} *`);
    out.add(`0 9 15 ${mo} *`);
    out.add(`0 9 * ${mo} 1-5`);
  }

  // Structural shapes, which are about the expression rather than any one field.
  for (const e of [
    "", "   ", "0 9 * *", "0 9 * * * *", "0 0 9 * * *",
    "@daily", "@hourly", "@every 1h", "@reboot",
    "not a cron", "0 9 * * 1 # comment",
    "  0  9  *  *  *  ",
  ]) {
    out.add(e);
  }

  // The TZ= / CRON_TZ= prefix, which robfig reads BEFORE the fields and whose
  // zone then overrides the schedule's timezone column. Prefix shapes crossed
  // with bodies of every disposition: structurable, advanced-only, and invalid.
  const TZ_PREFIXES = [
    "TZ=UTC", "CRON_TZ=Asia/Tokyo", "TZ=Local", "TZ=",
    "TZ=Bogus/Zone", "TZ=A=B", "tz=UTC", "TZ=UTC\t",
  ];
  const TZ_BODIES = [
    "0 9 * * *", "30 */3 * * 1-5", "0 9 1,15 * *", "?/2 ?/2 * * ?/2",
    "not a cron", "0 9 * *", "", "@daily",
  ];
  for (const p of TZ_PREFIXES) {
    for (const b of TZ_BODIES) out.add(`${p} ${b}`);
  }
  // And the shapes that are about the prefix itself: no schedule after it (the
  // robfig v3.0.1 panic the server guards), trailing space only, a second
  // prefix behind the first (robfig strips ONE and reads the next as a field),
  // and a leading space (not a prefix to robfig at all).
  for (const e of [
    "TZ=UTC", "CRON_TZ=Asia/Tokyo", "TZ=", "TZ=UTC ", "TZ=UTC   ",
    "TZ=UTC TZ=UTC 0 9 * * *", "TZ=UTC CRON_TZ=Asia/Tokyo 0 9 * * *",
    " TZ=UTC 0 9 * * *",
  ]) {
    out.add(e);
  }

  return [...out];
}

const CORPUS = corpus();

// Why the editor refuses an expression the backend would have accepted. These
// are the ONLY legitimate reasons: every valid expression that lands in
// advanced-only must be explained by one of them, or it is a corner of the
// grammar the model could have handled and quietly does not. The closure test at
// the bottom enforces exactly that, which is what keeps this list honest — a
// reason nobody can state is a gap, not a decision.
const EXCLUSIONS: Array<{ why: string; holds: (spec: Spec, fields: string[]) => boolean }> = [
  {
    // Commas around nothing (","). robfig drops the empty elements and is left
    // with a field selecting no value at all — legal, and the schedule simply
    // never fires. The model has no empty set to hold that (the chips keep at
    // least one day), so it stays advanced, where the raw text survives and the
    // server preview says "no upcoming runs" — which is the honest answer.
    why: "a field that selects no value at all, so the schedule never fires",
    holds: (s) => [s.minute, s.hour, s.dom, s.month, s.dow].some((f) => f.values.size === 0),
  },
  {
    why: "minute is a list, a range, or a step anchored off zero — the model has no minute-offset slot",
    holds: (_s, f) => {
      const m = f[0]!.replace(/^\?(?=\/|$)/, "*");
      if (m === "*") return false;
      if (/^\d{1,2}$/.test(m)) return false;
      return !/^(\*|0)\/\d{1,2}$/.test(m);
    },
  },
  {
    // A bare hour with a step ("9/2") is NOT excluded: robfig reads it as 9-23/2,
    // which is a window the model holds — so only a genuine LIST is left here.
    why: "hour is a list",
    holds: (_s, f) => {
      const h = f[1]!.replace(/^\?(?=\/|$)/, "*");
      return !(
        h === "*" ||
        /^\d{1,2}$/.test(h) ||
        /^\d{1,2}-\d{1,2}(\/\d{1,2})?$/.test(h) ||
        /^\*\/\d{1,2}$/.test(h) ||
        /^\d{1,2}\/\d{1,2}$/.test(h)
      );
    },
  },
  {
    why: "a minute step and an hour step at once — the model's interval has one step dimension",
    holds: (_s, f) => {
      const m = f[0]!.replace(/^\?(?=\/|$)/, "*");
      const minuteStepped = m === "*" || /^(\*|0)\/\d{1,2}$/.test(m);
      const hourStep = /\/(\d{1,2})$/.exec(f[1]!);
      return minuteStepped && hourStep !== null && parseInt(hourStep[1]!, 10) !== 1;
    },
  },
  {
    why: "day-of-month is anything but a single day",
    holds: (_s, f) => !(f[2] === "*" || /^\d{1,2}$/.test(f[2]!)),
  },
  {
    why: "a pinned day-of-month beside a restricted day-of-week — cron ORs them, the model can only intersect",
    holds: (_s, f) => f[2] !== "*" && f[4] !== "*",
  },
  { why: "month is pinned", holds: (_s, f) => f[3] !== "*" },
  {
    // "TZ=Local 0 9 * * *": legal to LoadLocation — it means the SERVER host's
    // zone — but no browser zone, so the picker has no entry to show for it.
    // Extraction would put a name in a control that cannot hold it; verbatim
    // advanced keeps the schedule running exactly as written instead.
    why: "an embedded timezone the picker cannot offer",
    holds: (s) => s.tz !== null && canonicalPickerZone(s.tz) === null,
  },
];

// Four reasons used to sit at the end of that list — or would have needed a
// place on it — and no longer do, because the editor stopped needing them:
//
//   "?" in any field — the backend reads it as a wildcard, and so does the editor
//   now (normalizeField rewrites it to "*", which is what it means).
//
//   A wildcard with an upper end ("*-19", "?-5") — the backend decides a range is
//   the full span from its low end alone and never reads the rest, so these mean
//   "*" too, and normalizeRange rewrites them to it. The editor echoes them into
//   its controls and saves them back in the canonical form. This one was invisible
//   until the reference stopped being tidier than robfig: while the oracle called
//   them syntax errors, the closure check below had nothing to explain.
//
//   A step wider than its own field ("*/65" on minutes, "*/24" on hours) — legal,
//   degenerate, and selecting only the range's first value, so it means a fixed
//   value, which the model has always had a slot for. The editor collapses it to
//   that value rather than greying itself out over a schedule it understands.
//
//   A list with a step-1 wildcard among its parts ("0,*", "1,*", "JAN,*") — robfig
//   ORs a list's parts together, and the wildcard part spans the whole field with
//   its star bit set, so the field means "*" outright, the AND-or-OR choice
//   between dom and dow included. collapseWildcardList rewrites it to "*" — but
//   only once every part is seen to parse, because "*,abc" and "*,60" are syntax
//   errors to the server and must stay ones to the editor. This family never had
//   an entry above: it was the gap a review found, in the corpus and the model at
//   once, which is what the guard on wildcard-list forms up in the corpus test
//   now keeps found.
//
//   A `TZ=` / `CRON_TZ=` prefix naming a zone the picker could offer — the
//   backend reads the zone off the front of the expression and lets it override
//   the timezone column, so the editor extracts it into the timezone field
//   (extractTimezonePrefix), where the picker shows it and the column becomes
//   the one source of truth. The shapes it deliberately leaves verbatim — no
//   space after the prefix (the robfig v3.0.1 panic), a second prefix behind
//   the first — are server rejections, not model limits, so they need no entry
//   above either; a zone the picker cannot hold ("TZ=Local") is the one prefix
//   shape that IS a model limit, and has its entry in the list.
//
// All were refusals to normalize, not limits of the model — the difference this
// list exists to keep honest. What is left above is the model's actual shape: it
// holds one day rule, one time rule and one step, and every reason there names a
// schedule that needs more than that.

/** The fields an exclusion reasons about, written as what they MEAN rather than
 *  as the punctuation they happen to carry — and the meaning comes from the
 *  reference parser, not from the editor, so a clause can never explain away a
 *  gap by agreeing with the code that made it.
 *
 *  A field the reference reads as a wildcard is spelled "*", whatever it was
 *  written as ("?", "*", "*\/1"). A field it reads as a single value is spelled
 *  as that value, whatever it was written as ("09", "9-9", "*\/65" → "0"). The
 *  rest keeps its text, with empty list elements dropped and numbers canonicalized
 *  the way strings.FieldsFunc and strconv.Atoi drop and canonicalize them.
 *
 *  Reading the raw text instead is how a stray comma sneaks past: "0," is the
 *  single value 0, but to a clause that matches "anything which is not a plain
 *  number" it reads as a list, and the closure check then explains away a real
 *  gap with a reason that was never true. That is exactly how a fix normalizing
 *  only day-of-week survived this suite — and "009" reading as "not a plain
 *  number" is the same trap with digits instead of commas. */
function meaningfulFields(expr: string, spec: Spec): string[] {
  const fields = [spec.minute, spec.hour, spec.dom, spec.month, spec.dow];
  return expr
    .trim()
    .split(/\s+/)
    .map((field, i) => {
      const parsed = fields[i]!;
      if (parsed.star) return "*";
      if (parsed.values.size === 1) return String([...parsed.values][0]);
      return field
        .split(",")
        .filter((part) => part.length > 0)
        .map((part) =>
          part.replace(/\+?\d+/g, (num) => {
            const n = Number(num);
            return Number.isSafeInteger(n) ? String(n) : num;
          }),
        )
        .join(",");
    });
}

describe("cron grammar — the editor against a reference robfig parser", () => {
  it("covers every token form of every field", () => {
    // A guard on the corpus itself: if a future edit guts these lists, the
    // invariants below would still pass, vacuously.
    expect(CORPUS.length).toBeGreaterThan(10_000);
    expect(CORPUS.filter((e) => reference(e) !== null).length).toBeGreaterThan(3000);
    expect(CORPUS.filter((e) => reference(e) === null).length).toBeGreaterThan(3000);
    // Both dimensions must actually be crossed — the omission that let a mutant
    // dropping the day-of-month for interval times slip through.
    expect(CORPUS).toContain("0 */2 15 * *");
    expect(CORPUS).toContain("*/10 9-18 15 * *");
    expect(CORPUS).toContain("0 9-21/2 * * 1-5");
    // Atoi's lexical variance must be present in every field.
    expect(CORPUS).toContain("009 9 * * *");
    expect(CORPUS).toContain("0 9 * * +1");
    // As must the wildcard-low-end forms, in every field — the quirk the
    // reference above used to be tidier than.
    expect(CORPUS).toContain("*-30 9 * * *");
    expect(CORPUS).toContain("0 *-19 * * *");
    expect(CORPUS).toContain("0 9 *-15 * *");
    expect(CORPUS).toContain("0 9 * *-6 *");
    expect(CORPUS).toContain("0 9 * * ?-5");
    // Lists carrying a wildcard part, in every field — the family that once
    // slipped past the closure test below by not being in the corpus at all.
    expect(CORPUS).toContain("0,* 9 * * *");
    expect(CORPUS).toContain("0 9,* * * *");
    expect(CORPUS).toContain("0 9 1,* * *");
    expect(CORPUS).toContain("0 9 * 1,* *");
    expect(CORPUS).toContain("0 9 * * 1,*");
    expect(CORPUS).toContain("*,abc 9 * * *");
    // And the timezone prefix, in its accepted, misspelled and panic shapes.
    expect(CORPUS).toContain("TZ=UTC 0 9 * * *");
    expect(CORPUS).toContain("CRON_TZ=Asia/Tokyo 30 */3 * * 1-5");
    expect(CORPUS).toContain("TZ=Bogus/Zone 0 9 * * *");
    expect(CORPUS).toContain("TZ=UTC");
    expect(CORPUS).toContain("tz=UTC 0 9 * * *");
  });

  it("reads the timezone prefix the way the server does", () => {
    // The oracle's prefix claims, pinned the way the wildcard low end is below:
    // the zone is read up to the first literal space and overrides the timezone
    // column; a prefix with no space after it is refused (robfig v3.0.1 panics
    // there — parser.go:99 — and the server's guard turns it into a rejection);
    // the detection is case-sensitive and untrimmed; a second prefix is a field.
    expect(reference("TZ=UTC 0 9 * * *")).toMatchObject({ tz: "UTC", tzValid: true });
    expect(reference("CRON_TZ=Asia/Tokyo 0 9 * * *")).toMatchObject({ tz: "Asia/Tokyo" });
    expect(reference("TZ= 0 9 * * *")).toMatchObject({ tz: "UTC", tzValid: true });
    expect(reference("TZ=Bogus/Zone 0 9 * * *")).toMatchObject({ tzValid: false });
    expect(reference("TZ=UTC")).toBeNull();
    expect(reference("CRON_TZ=Asia/Tokyo")).toBeNull();
    expect(reference("TZ=")).toBeNull();
    expect(reference("tz=UTC 0 9 * * *")).toBeNull();
    expect(reference(" TZ=UTC 0 9 * * *")).toBeNull();
    expect(reference("TZ=UTC TZ=UTC 0 9 * * *")).toBeNull();
  });

  it("reads a wildcard's low end the way the server does", () => {
    // The oracle's own claim, pinned: robfig takes "*-19" for the full span and
    // never reads what came after the "-". If this ever stops being true of the
    // server, this is the test that says so — and the corpus below is judged
    // against a grammar that has drifted.
    expect(reference("0 *-19 * * *")?.hour.values.size).toBe(24);
    expect(reference("0 ?-19 * * *")?.hour.star).toBe(true);
    expect(reference("0 *-19-25 * * *")?.hour.values.size).toBe(24);
    expect([...(reference("0 *-19/2 * * *")?.hour.values ?? [])]).toEqual([
      0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22,
    ]);
    // Only the LOW end: a "?" anywhere else is the syntax error it always was.
    expect(reference("0 9-? * * *")).toBeNull();
  });

  it("never throws, whatever it is handed", () => {
    for (const expr of CORPUS) {
      expect(() => parseCron(expr, "UTC"), expr).not.toThrow();
    }
  });

  it("never structures an expression whose fields the server cannot parse", () => {
    // The inverse is the dangerous one: a schedule the editor shows in its
    // controls, lets the user save, and the backend then refuses — or worse,
    // silently reinterprets.
    const wronglyAccepted = CORPUS.filter(
      (e) => reference(e) === null && parseCron(e, "UTC").raw === null,
    );
    expect(wronglyAccepted).toEqual([]);
  });

  it("keeps an embedded zone in the config, so the server still judges the pair", () => {
    // What extraction must never do is drop or replace the zone: that would
    // turn a rejected expression into an accepted schedule in a different
    // timezone. A zone the picker cannot hold ("TZ=Bogus/Zone", "TZ=Local")
    // is not extracted at all — the expression stays verbatim in `raw`, and
    // toCron passes a self-prefixed raw through untouched, so the zone still
    // reaches the server exactly as written.
    // A sentinel that matches no embedded zone, so extraction and mere
    // pass-through of the parameter cannot be mistaken for one another. Both
    // directions are pinned: everywhere the syntactic rule extracts, the config
    // carries that zone — and everywhere it must not (no space after the
    // prefix, a second prefix), the parameter comes through untouched.
    const wrongTz = CORPUS.filter((e) => {
      const p = extractedForEcho(e);
      return parseCron(e, "Test/Sentinel").timezone !== (p?.tz ?? "Test/Sentinel");
    });
    expect(wrongTz).toEqual([]);
  });

  it("never structures a config the model's own domain cannot hold", () => {
    // Semantic equivalence cannot see this class of bug: a config with an
    // hour interval of 24 serializes back to the same "*/24" it came from and
    // fires at the same instants — but model.ts bounds intervals (hours 1-23,
    // minutes 1-59) and the interval select renders nothing past them, so the
    // user gets a control showing an impossible value. Every structured field
    // is checked against the domain the model declares, not against cron.
    const validTime = (t: string) => /^([01]\d|2[0-3]):[0-5]\d$/.test(t);
    const outOfDomain: Array<{ expr: string; why: string }> = [];
    for (const expr of CORPUS) {
      const c = parseCron(expr, "UTC");
      if (c.raw !== null) continue;
      const bad = (why: string) => outOfDomain.push({ expr, why });
      const { time, days } = c;
      if (time.kind === "at") {
        if (!validTime(time.time)) bad("at-time malformed");
      } else {
        const maxInterval = time.unit === "hours" ? 23 : 59;
        if (!Number.isInteger(time.interval) || time.interval < 1 || time.interval > maxInterval) {
          bad(`interval ${time.interval} outside 1-${maxInterval}`);
        }
        if (!Number.isInteger(time.minute) || time.minute < 0 || time.minute > 59) {
          bad(`minute ${time.minute} outside 0-59`);
        }
        if (time.window !== null && (!validTime(time.window.from) || !validTime(time.window.to))) {
          bad("window malformed");
        }
      }
      if (days.kind === "weekly") {
        const chips = days.daysOfWeek;
        if (chips.length === 0) bad("weekly with no days");
        if (chips.some((d) => !Number.isInteger(d) || d < 0 || d > 6)) bad("day outside 0-6");
        const canonical = [...new Set(chips)].toSorted((a, b) => a - b);
        if (JSON.stringify(canonical) !== JSON.stringify(chips)) bad("days not deduped ascending");
      } else if (days.kind === "monthly") {
        if (!Number.isInteger(days.dayOfMonth) || days.dayOfMonth < 1 || days.dayOfMonth > 31) {
          bad(`dayOfMonth ${days.dayOfMonth} outside 1-31`);
        }
      }
    }
    expect(outOfDomain).toEqual([]);
  });

  it("preserves the schedule's meaning through every structurable round-trip", () => {
    // The heart of it: for everything the editor claims it can represent, the
    // PAIR it hands back — the cron AND the timezone, since an embedded zone
    // moves between them — must fire at exactly the same instants as the
    // expression it was given. Set equality over the whole domain, not a spot
    // check; the zone compared by name, since the fields are zone-relative.
    const drifted: Array<{ expr: string; became: string; tz: string }> = [];
    for (const expr of CORPUS) {
      const config = parseCron(expr, "UTC");
      if (config.raw !== null) continue;
      const before = reference(expr);
      const after = reference(toCron(config));
      if (
        before === null ||
        after === null ||
        !sameSchedule(before, after) ||
        config.timezone !== (before.tz ?? "UTC")
      ) {
        drifted.push({ expr, became: toCron(config), tz: config.timezone });
      }
    }
    expect(drifted).toEqual([]);
  });

  it("hands back an unrepresentable expression as it was written, minus only an extracted prefix", () => {
    // The prefix moves into the timezone field; everything else survives
    // verbatim. `raw` for a prefixed expression is the fields alone — saving
    // the pair (raw, timezone) is the same schedule the prefix spelled.
    const mangled = CORPUS.filter((e) => {
      const c = parseCron(e, "Test/Sentinel");
      if (c.raw === null) return false;
      const p = extractedForEcho(e);
      const expectedRaw = p === null ? e : p.rest;
      return c.raw !== expectedRaw || cronFields(c) !== expectedRaw;
    });
    expect(mangled).toEqual([]);
  });

  it("is idempotent: a second pass changes nothing", () => {
    // The second pass gets the pair the first one produced — the timezone is
    // part of the round-trip now that a prefix extracts into it.
    const unstable = CORPUS.filter((expr) => {
      const once = parseCron(expr, "UTC");
      const twice = parseCron(toCron(once), once.timezone);
      return JSON.stringify(twice) !== JSON.stringify(once);
    });
    expect(unstable).toEqual([]);
  });

  it("sends a valid expression to advanced-only for a documented reason, never by accident", () => {
    // An expression the backend accepts, that the editor nonetheless refuses to
    // show in its controls, and that none of the reasons in EXCLUSIONS explains,
    // is a corner of the grammar the model could have handled and quietly does
    // not — the user gets greyed-out controls and a raw-cron box for a schedule
    // that runs perfectly well. Enumerating the difference is what turns "did we
    // remember that shape?" from a question about someone's care into a
    // mechanical diff.
    const unexplained: string[] = [];
    for (const expr of CORPUS) {
      const spec = reference(expr);
      if (!serverAccepts(spec)) continue;
      if (parseCron(expr, "UTC").raw === null) continue;
      // The fields an exclusion reasons about are the BODY's — an extracted
      // prefix is not a field, and would shift all five off by one.
      if (!EXCLUSIONS.some((x) => x.holds(spec, meaningfulFields(spec.body, spec)))) {
        unexplained.push(expr);
      }
    }
    expect(unexplained).toEqual([]);
  });
});
