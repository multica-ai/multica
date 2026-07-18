import type { DayPattern, ScheduleConfig, TimePattern } from "./model";
import { consecutiveRuns, getDefaultScheduleConfig, pad2, timeParts } from "./model";

// The backend parser (robfig/cron v3, 5-field) accepts a closed grammar per
// field: `*`, `?`, integers, month/day names, `-` ranges, `/` steps and `,`
// lists. The structured editor round-trips only the subset below; everything
// else stays verbatim in `raw` (advanced-only mode).

const DOW_NAMES: Record<string, number> = {
  SUN: 0,
  MON: 1,
  TUE: 2,
  WED: 3,
  THU: 4,
  FRI: 5,
  SAT: 6,
};

const MONTH_NAMES: Record<string, number> = {
  JAN: 1,
  FEB: 2,
  MAR: 3,
  APR: 4,
  MAY: 5,
  JUN: 6,
  JUL: 7,
  AUG: 8,
  SEP: 9,
  OCT: 10,
  NOV: 11,
  DEC: 12,
};

interface FieldBounds {
  min: number;
  max: number;
  names?: Record<string, number>;
}

const MINUTE: FieldBounds = { min: 0, max: 59 };
const HOUR: FieldBounds = { min: 0, max: 23 };
const DOM: FieldBounds = { min: 1, max: 31 };
const MONTH: FieldBounds = { min: 1, max: 12, names: MONTH_NAMES };
const DOW: FieldBounds = { min: 0, max: 6, names: DOW_NAMES };

/** One part of a field, as robfig's getRange reads it. */
interface RangePart {
  lo: number;
  hi: number;
  step: number;
  /** The range was written as a wildcard ("*", or the "?" normalizeField rewrites
   *  to one) rather than spelled out. Not robfig's star BIT, which it clears for
   *  any step above 1: that bit decides whether dom and dow are ANDed or ORed, and
   *  parseCron settles that question on the field text before it gets here — it
   *  structures a pinned day-of-month only when day-of-week is written "*". What
   *  this flag carries is the text, because that is what the model echoes: an hour
   *  written "*" is "all day", and one written "0-23" is a window that happens to
   *  span it. */
  wildcard: boolean;
  /** A step was written, whatever its value. "*" and "*\/1" select the same
   *  values but are not the same text, and the collapse rules turn on which. */
  stepped: boolean;
  /** The range was written with both ends ("9-21"), not as a bare value. */
  explicitRange: boolean;
}

function fieldValue(s: string, bounds: FieldBounds): number | null {
  const named = bounds.names?.[s.toUpperCase()];
  if (named !== undefined) return named;
  if (!/^\d+$/.test(s)) return null;
  const n = Number(s);
  // Numbers past the safe-integer range overflow the backend's Atoi and are a
  // syntax error to it; out-of-bounds ones are one to its bounds check.
  if (!Number.isSafeInteger(n) || n < bounds.min || n > bounds.max) return null;
  return n;
}

/**
 * `<range>` or `<range>/<step>`, where `<range>` is `*`, a value, or `lo-hi` —
 * the whole of robfig's per-part grammar, in one place. Fields differ only in
 * the bounds and names they pass in, never in what the grammar means: a bare
 * value carrying a step runs to the field's max ("9/2" on hours is 9-23/2).
 *
 * The step is deliberately unbounded here, as it is in robfig: a step wider than
 * the range it steps over is legal and simply selects that range's first value
 * ("*\/100" on days is Sunday). Callers whose model slot cannot hold a step that
 * wide reject it themselves — they know what they can hold; the grammar does not.
 */
function parseRangePart(part: string, bounds: FieldBounds): RangePart | null {
  const [rangeStr, stepStr, ...rest] = part.split("/");
  if (rest.length > 0 || !rangeStr) return null;

  let step = 1;
  if (stepStr !== undefined) {
    if (!/^\d+$/.test(stepStr)) return null;
    step = Number(stepStr);
    if (!Number.isSafeInteger(step) || step < 1) return null;
  }
  const stepped = stepStr !== undefined;

  // normalizeField has already rewritten every wildcard range to "*".
  if (rangeStr === "*") {
    return {
      lo: bounds.min,
      hi: bounds.max,
      step,
      wildcard: true,
      stepped,
      explicitRange: false,
    };
  }

  const [loStr, hiStr, ...restRange] = rangeStr.split("-");
  if (restRange.length > 0 || !loStr) return null;
  const lo = fieldValue(loStr, bounds);
  if (lo === null) return null;

  let hi: number;
  if (hiStr === undefined) {
    // A bare value runs to the field's max when it carries a step ("9/2" is
    // 9-23/2), and is just itself when it does not.
    hi = stepped ? bounds.max : lo;
  } else {
    const hiVal = fieldValue(hiStr, bounds);
    if (hiVal === null) return null;
    hi = hiVal;
  }
  if (hi < lo) return null;

  return { lo, hi, step, wildcard: false, stepped, explicitRange: hiStr !== undefined };
}

/** The whole field as one pinned value — "9", and nothing that merely selects a
 *  single value by other means ("9-9", "*\/100"), which the collapse rewrites to
 *  this shape first when the model has a slot for it. */
function plainValue(field: string, bounds: FieldBounds): number | null {
  const r = parseRangePart(field, bounds);
  if (r === null || r.wildcard || r.stepped || r.explicitRange) return null;
  return r.lo;
}

/** Expand a dow field into a sorted, deduped day set, or null when it falls
 *  outside the echoable grammar. Digits, names, ranges, steps and lists are
 *  all enumerable — dow is structurable whenever robfig would accept it, a step
 *  wider than the week ("*\/100" → Sunday) included. */
function expandDow(field: string): number[] | null {
  const days = new Set<number>();
  // `field` arrives normalized (see normalizeField), so an empty part here is a
  // genuinely malformed expression, not a stray comma.
  for (const part of field.split(",")) {
    const r = parseRangePart(part, DOW);
    if (r === null) return null;
    for (let d = r.lo; d <= r.hi; d += r.step) days.add(d);
  }
  if (days.size === 0) return null;
  return Array.from(days).toSorted((a, b) => a - b);
}

/** Serialize a day set as maximal consecutive runs: [1,2,3,4,5] → "1-5",
 *  [0,1,2,4] → "0-2,4", [0,2,4,6] → "0,2,4,6". Any pair is worth a range here —
 *  "0-1" is shorter than "0,1". */
function dowFieldFromDays(days: number[]): string {
  return consecutiveRuns(days)
    .map(([lo, hi]) => (hi > lo ? `${lo}-${hi}` : `${lo}`))
    .join(",");
}

/** The hour as an interval and an optional window. The model's interval is an
 *  hour count, so a step it cannot hold has no every-N-hours form — a degenerate
 *  one is already a fixed value by then, rewritten by collapseDegenerateRange. */
function parseHourPattern(
  field: string,
): { interval: number; range: { lo: number; hi: number } | null } | null {
  const r = parseRangePart(field, HOUR);
  if (r === null || r.step > HOUR.max) return null;
  // "*" and "0-23" select the same hours, and the model holds all day exactly one
  // way — as the absence of a window. The editor's window is always on screen now,
  // so there is no toggle state left for the two spellings to differ in: a window
  // that spans the day would only leave the reset control lit on a schedule that
  // is already all day, and hand the same schedule two structured forms.
  const spansDay = r.wildcard || (r.lo === HOUR.min && r.hi === HOUR.max);
  return { interval: r.step, range: spansDay ? null : { lo: r.lo, hi: r.hi } };
}

/** `*` → 1, `*\/N` and `0/N` → N. Anchored steps ("15/5") would need a minute
 *  offset the model does not carry, so they stay advanced-only. */
function parseMinuteInterval(field: string): number | null {
  const r = parseRangePart(field, MINUTE);
  if (r === null || r.explicitRange || r.step > MINUTE.max) return null;
  // The step must run from the top of the hour: "*/5" and "0/5" are the same set,
  // "15/5" is one the model has no offset slot for. A bare "0" is a fixed minute,
  // not an interval — but parseTimeFields reads that before it ever gets here.
  if (r.lo !== MINUTE.min) return null;
  if (!r.stepped && !r.wildcard) return null;
  return r.step;
}

function parseTimeFields(minuteField: string, hourField: string): TimePattern | null {
  const plainMinute = plainValue(minuteField, MINUTE);

  // Fixed minute: either a fixed time, or an every-N-hours pattern.
  if (plainMinute !== null) {
    const plainHour = plainValue(hourField, HOUR);
    if (plainHour !== null) {
      return { kind: "at", time: `${pad2(plainHour)}:${pad2(plainMinute)}` };
    }
    const hour = parseHourPattern(hourField);
    if (hour === null) return null;
    return {
      kind: "every",
      unit: "hours",
      interval: hour.interval,
      minute: plainMinute,
      window:
        hour.range === null
          ? null
          : {
              from: `${pad2(hour.range.lo)}:${pad2(plainMinute)}`,
              to: `${pad2(hour.range.hi)}:${pad2(plainMinute)}`,
            },
    };
  }

  // Wildcard/stepped minute: an every-N-minutes pattern with an hour-granular
  // window. The model carries a single step, so a real hour step cannot be
  // combined with a minute step — but a step of 1 selects every hour, which is
  // just the unstepped field written out ("*/1" ≡ "*").
  const interval = parseMinuteInterval(minuteField);
  if (interval === null) return null;
  let window: { from: string; to: string } | null;
  const hour = parseHourPattern(hourField);
  if (hour === null) return null;
  if (hour.range === null) {
    // An all-day hour field — "*/N", or the "0-23/N" that means the same — is a
    // second step dimension once its step is real; the model has one step.
    if (hour.interval !== 1) return null;
    window = null;
  } else {
    const { lo, hi } = hour.range;
    // A step wider than the range it steps over never gets past that range's
    // first hour, so it selects that hour alone — a one-hour window, not a
    // second step. "9-9/3" and "9-21/23" both mean 09:00–09:59 here.
    const selectsOneHour = hour.interval > hi - lo;
    if (!selectsOneHour && hour.interval !== 1) return null;
    window = { from: `${pad2(lo)}:00`, to: `${pad2(selectsOneHour ? lo : hi)}:59` };
  }
  return { kind: "every", unit: "minutes", interval, minute: 0, window };
}

/** The backend accepts three kinds of lexical variance the parsers below do not:
 *  it splits list fields with strings.FieldsFunc, which DROPS empty elements
 *  ("1-5," is Mon–Fri to it, ",9" is hour 9, "1,,5" is {Mon, Fri}), it parses
 *  every number with strconv.Atoi, which takes leading zeros and a plus sign
 *  ("009", "+9" and "00/05" are 9, 9 and 0/5 to it), and it decides a range is a
 *  wildcard on its LOW END ALONE — see normalizeRange. Every field is normalized
 *  here, once, before any of the parsers below see it: a parser left to remember
 *  this rule itself would, when it forgets, grey out the whole editor on a
 *  schedule the server runs perfectly happily. */
function normalizeField(field: string): string {
  return field
    .split(",")
    .filter((part) => part.length > 0)
    .map(normalizeRange)
    .map((part) =>
      part.replace(/\+?\d+/g, (num) => {
        // Numbers past the safe-integer range overflow Atoi and are a syntax
        // error server-side; left alone, the parsers reject them the same way.
        const n = Number(num);
        return Number.isSafeInteger(n) ? String(n) : num;
      }),
    )
    .join(",");
}

/** robfig tests a range's LOW END for "*" or "?" (parser.go:261) and, when it
 *  matches, takes the whole range as the field's full span — never looking at
 *  what followed the "-". So "?" is "*", and so are "*-19", "?-5" and even
 *  "*-19-25": the upper end is not merely unchecked, it is unread. A step
 *  survives ("?/2" is "*\/2", "*-19/2" is "*\/2" — the low end sets the span, the
 *  step still walks it), and only the low end is rewritten here, so "9-?" stays
 *  the syntax error it is to the server.
 *
 *  Odd, and load-bearing: these are wildcards to the parser that will run the
 *  schedule, so an editor that called them unrepresentable would grey out its
 *  controls over an expression that means "every hour". */
function normalizeRange(part: string): string {
  const slash = part.indexOf("/");
  const range = slash === -1 ? part : part.slice(0, slash);
  const step = slash === -1 ? "" : part.slice(slash);
  const low = range.split("-")[0];
  return low === "*" || low === "?" ? `*${step}` : part;
}

/** A step wider than the range it steps over never gets past that range's first
 *  value, so it means a fixed value: "*\/65" on minutes is 0, "*\/24" on hours is
 *  0, "10-20/30" is 10, "*\/40" on day-of-month is the 1st. The server runs all of
 *  these happily, and a fixed value is something the model has always had a slot
 *  for. Rewriting them as that value is what lets the controls take them; without
 *  it the editor greys itself out over a schedule it fully understands, and tells
 *  the user — untruthfully — that the controls cannot represent it.
 *
 *  `rangeSlot` marks a field the model holds as a range rather than as a value:
 *  hour, and only hour. Such a field is never collapsed while the parser can still
 *  read it exactly — neither a stepless "9-9" (the one-hour window the editor
 *  writes when a dragged window start meets its end) nor a stepped "9-9/3" (the
 *  same window at interval 3) may come back as a fixed time, or the every/unit/
 *  window controls the user set are silently discarded on reload. Only a step past
 *  what the model's interval can hold (> max) still collapses there, since
 *  parseHourPattern would refuse it and the expression would grey the editor out
 *  instead. Minute and day-of-month hold a value and not a range, so they collapse
 *  in every form, "5-5" included.
 *
 *  Safe on day-of-month, where the star bit decides whether dom and dow are ORed
 *  or ANDed: robfig clears that bit for any step above 1 (parser.go:297), so
 *  "*\/40" is already a restricted field, and "1" is another — the collapse cannot
 *  flip "the 1st, or any Monday" into "the 1st, if it is a Monday". Month is left
 *  alone: any pinned month is beyond the model regardless. */
function collapseDegenerateRange(
  field: string,
  bounds: FieldBounds,
  rangeSlot: boolean,
): string {
  return field
    .split(",")
    .map((part) => {
      const r = parseRangePart(part, bounds);
      // Not the grammar's shape at all: leave it be, and let the field's own
      // parser refuse it and send the expression to advanced-only.
      if (r === null) return part;
      // A range-slot field keeps every range its parser can still read exactly:
      // stepless ("9-9"), and stepped within the model's interval ("9-9/3").
      if (rangeSlot && !r.stepped) return part;
      if (rangeSlot && r.explicitRange && r.step <= bounds.max) return part;
      return r.step > r.hi - r.lo ? String(r.lo) : part;
    })
    .join(",");
}

/** A list with a step-1 wildcard element among its parts selects every value
 *  the field has, star bit included: robfig ORs a list's parts together, the
 *  wildcard part already spans the field, and a step of 1 is what keeps its
 *  star bit set (parser.go:297) — so "1,*" IS "*", right down to the AND-or-OR
 *  choice that bit drives between dom and dow. The parsers below read lists
 *  only where the model enumerates them (day-of-week), so without this rewrite
 *  a "0,*" minute greys the whole editor out over a schedule that means
 *  "every minute".
 *
 *  Collapsed only when EVERY part parses: "*,abc" and "*,60" are syntax errors
 *  to the server, and a collapse that read just the wildcard part would
 *  structure an expression the server rejects. A list whose wildcard parts all
 *  carry real steps ("*\/2,*\/3") spans nothing by itself and is left for the
 *  field's own parser to judge. */
function collapseWildcardList(field: string, bounds: FieldBounds): string {
  const parts = field.split(",");
  if (parts.length < 2) return field;
  const ranges = parts.map((part) => parseRangePart(part, bounds));
  if (ranges.some((r) => r === null)) return field;
  return ranges.some((r) => r!.wildcard && r!.step === 1) ? "*" : field;
}

/** The two prefixes robfig's Parse reads a timezone from. */
export function hasTimezonePrefix(s: string): boolean {
  return s.startsWith("TZ=") || s.startsWith("CRON_TZ=");
}

/** robfig's Parse reads an optional `TZ=` / `CRON_TZ=` prefix off the
 *  expression before the fields — case-sensitively, on the untrimmed text, up
 *  to the FIRST LITERAL SPACE (parser.go, `strings.Index(spec, " ")`) — and the
 *  embedded zone then overrides the schedule's own timezone column: both go
 *  through the same LoadLocation, so `CRON_TZ=Asia/Tokyo 0 9 * * *` with the
 *  column at UTC fires at 09:00 Tokyo. A prefixed expression IS the pair
 *  (timezone, fields), and extracting it into the editor's timezone field is
 *  exact — it is what keeps the timezone picker truthful over an expression
 *  that would otherwise silently overrule it.
 *
 *  Null when there is nothing robfig would read as a prefix, and for the shapes
 *  it would read but the editor must not touch:
 *  - no space at all ("TZ=UTC"): robfig v3.0.1 panics on it (parser.go:99), so
 *    it stays verbatim in advanced, where the server (guarded) calls it invalid;
 *  - a second prefix right behind the first: robfig strips ONE prefix and reads
 *    the second as a field, so extraction would quietly turn a rejected
 *    expression into an accepted one;
 *  - a zone the picker could not have offered (isPickerZone). config.timezone
 *    is a picker value and a formatter argument, so only names the browser can
 *    hold there are extracted. "TZ=Local 0 9 * * *" — legal server-side, where
 *    it means the SERVER host's zone — stays verbatim in advanced and keeps
 *    running exactly as written; so does a zone the server would reject, whose
 *    honest echo is the raw text under the server's own error. */
export function extractTimezonePrefix(expr: string): { timezone: string; rest: string } | null {
  if (!hasTimezonePrefix(expr)) return null;
  const space = expr.indexOf(" ");
  if (space === -1) return null;
  const name = expr.slice(expr.indexOf("=") + 1, space);
  const rest = expr.slice(space).trim();
  if (rest.length === 0) return null;
  if (hasTimezonePrefix(rest)) return null;
  // An empty name loads as UTC server-side (LoadLocation("")); say so.
  const timezone = canonicalPickerZone(name === "" ? "UTC" : name);
  if (timezone === null) return null;
  return { timezone, rest };
}

/** The zone as the picker's list spells it, or null when the browser cannot
 *  hold it at all — as the picker's value or as an Intl formatter argument.
 *  Intl reads zone names case-insensitively, so "asia/shanghai" is a real zone
 *  to it; putting THAT spelling in the config would seat a duplicate next to
 *  the list's own "Asia/Shanghai" entry. resolvedOptions canonicalizes the
 *  name, which is also the spelling the server's LoadLocation is guaranteed to
 *  load — lowercase only ever worked there by the grace of a case-insensitive
 *  filesystem. Where the browser and the server genuinely part ways ("Local",
 *  tab-ridden garbage), the browser is the authority here, because
 *  config.timezone is a browser-side value. */
function canonicalPickerZone(tz: string): string | null {
  try {
    return new Intl.DateTimeFormat(undefined, { timeZone: tz }).resolvedOptions().timeZone;
  } catch {
    return null;
  }
}

export function parseCron(expr: string, timezone: string): ScheduleConfig {
  // The prefix is extracted whether or not the rest is structurable: the zone
  // rides in `timezone` either way, and `raw` keeps only the fields. This is
  // the inverse of toCron, which puts the prefix back on the way out — and it
  // also hydrates legacy rows saved before expressions carried one, for which
  // the caller passes the timezone column as the fallback.
  const prefix = extractTimezonePrefix(expr);
  const tz = prefix === null ? timezone : prefix.timezone;
  const body = prefix === null ? expr : prefix.rest;
  const advanced: ScheduleConfig = {
    ...getDefaultScheduleConfig(tz),
    raw: body,
  };
  const parts = body.trim().split(/\s+/);
  if (parts.length !== 5) return advanced;
  const [rawMinute, rawHour, rawDom, rawMonth, rawDow] = parts.map(normalizeField) as [
    string,
    string,
    string,
    string,
    string,
  ];
  const minuteField = collapseDegenerateRange(collapseWildcardList(rawMinute, MINUTE), MINUTE, false);
  const hourField = collapseDegenerateRange(collapseWildcardList(rawHour, HOUR), HOUR, true);
  const domField = collapseDegenerateRange(collapseWildcardList(rawDom, DOM), DOM, false);
  const monthField = collapseWildcardList(rawMonth, MONTH);
  const dowField = collapseWildcardList(rawDow, DOW);

  if (monthField !== "*") return advanced;

  let days: DayPattern;
  if (domField === "*") {
    if (dowField === "*") {
      days = { kind: "every" };
    } else {
      const daysOfWeek = expandDow(dowField);
      if (daysOfWeek === null) return advanced;
      days = { kind: "weekly", daysOfWeek };
    }
  } else {
    // A pinned day-of-month is only structurable when dow is unrestricted:
    // cron treats dom+dow as OR, which the model cannot express.
    if (dowField !== "*") return advanced;
    const dayOfMonth = plainValue(domField, DOM);
    if (dayOfMonth === null) return advanced;
    days = { kind: "monthly", dayOfMonth };
  }

  const time = parseTimeFields(minuteField, hourField);
  if (time === null) return advanced;

  return { time, days, timezone: tz, raw: null };
}

/** The wire expression: the timezone rides IN the cron string, as a `TZ=`
 *  prefix, on every save and every preview. robfig reads an embedded zone
 *  before the schedule's own timezone column, so a bare expression next to the
 *  column is two sources of truth with a silent override rule between them —
 *  prefixing unconditionally leaves exactly one. The column is still written,
 *  as a mirror: display surfaces and the preview endpoint's invalid_timezone
 *  classification read it, and it always agrees with the prefix.
 *
 *  The one shape that is NOT prefixed is an advanced `raw` that already starts
 *  with a prefix of its own: that is a typed expression awaiting the server's
 *  verdict (see applyDraft), and stacking a second prefix on it would make
 *  robfig read the first as the zone and the second as a field. It goes out
 *  verbatim — the embedded zone wins server-side, which is what the text says. */
export function toCron(config: ScheduleConfig): string {
  const fields = cronFields(config);
  if (hasTimezonePrefix(fields)) return fields;
  // Leading/trailing whitespace would not survive the prefix — robfig (and
  // parseCron) trim the text after the zone, so prefixing would silently edit
  // the very characters that make such an expression the rejection it is —
  // and an empty expression has nothing to put one on.
  if (fields === "" || fields !== fields.trim()) return fields;
  // A zone name with a space in it would end at that space to robfig and hand
  // the remainder to the field parser. IANA names never contain one (the picker
  // could not have offered it), so this only guards a corrupt config — which
  // degrades to the bare expression the timezone column already covers.
  if (config.timezone === "" || config.timezone.includes(" ")) return fields;
  return `TZ=${config.timezone} ${fields}`;
}

/** The schedule's five fields alone — the editable text of the cron box, whose
 *  `TZ=` prefix is a fixed group-input segment drawn from config.timezone, not
 *  characters in the field. Advanced expressions round-trip verbatim here. */
export function cronFields(config: ScheduleConfig): string {
  if (config.raw !== null) return config.raw;

  let dom = "*";
  let dow = "*";
  if (config.days.kind === "monthly") {
    dom = String(config.days.dayOfMonth);
  } else if (config.days.kind === "weekly") {
    // The editor keeps at least one day selected; serialize defensively to
    // Monday rather than emit an empty field.
    dow = config.days.daysOfWeek.length > 0 ? dowFieldFromDays(config.days.daysOfWeek) : "1";
  }

  const { time } = config;
  let minuteField: string;
  let hourField: string;
  if (time.kind === "at") {
    const at = timeParts(time.time);
    minuteField = String(at.minute);
    hourField = String(at.hour);
  } else if (time.unit === "hours") {
    if (time.window === null) {
      minuteField = String(time.minute);
      hourField = time.interval === 1 ? "*" : `*/${time.interval}`;
    } else {
      const from = timeParts(time.window.from);
      const to = timeParts(time.window.to);
      minuteField = String(from.minute);
      hourField =
        time.interval === 1
          ? `${from.hour}-${to.hour}`
          : `${from.hour}-${to.hour}/${time.interval}`;
    }
  } else {
    minuteField = time.interval === 1 ? "*" : `*/${time.interval}`;
    if (time.window === null) {
      hourField = "*";
    } else {
      const from = timeParts(time.window.from);
      const to = timeParts(time.window.to);
      hourField = from.hour === to.hour ? String(from.hour) : `${from.hour}-${to.hour}`;
    }
  }
  return `${minuteField} ${hourField} ${dom} * ${dow}`;
}
