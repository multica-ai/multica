"use client";

import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type ComponentType,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { Clock, Pencil } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Input } from "@multica/ui/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupSelectTrigger,
  InputGroupText,
  InputGroupTimeInput,
} from "@multica/ui/components/ui/input-group";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { cronPreviewOptions } from "@multica/core/autopilots/queries";
import { ApiError } from "@multica/core/api";
import { timezoneOptions } from "../../../common/timezone-select";
import { useDebouncedValue } from "../../../common/use-debounced-value";
import { SegmentedToggle } from "../../../common/segmented-toggle";
import { formatInTimeZone } from "../../../common/format-in-time-zone";
import { TimezonePicker } from "../pickers/timezone-picker";
import { useT } from "../../../i18n";
import type { DayPattern, ScheduleConfig, TimePattern } from "./model";
import { DAY_KEYS, pad2, timeParts } from "./model";
import {
  cronFields,
  extractTimezonePrefix,
  hasTimezonePrefix,
  parseCron,
  toCron,
} from "./cron-mapping";
import { classifyScheduleRejection } from "./validate";
import { useDescribeSchedule } from "./describe";

export interface ScheduleEditorProps {
  value: ScheduleConfig;
  onChange: (value: ScheduleConfig) => void;
  wsId: string;
  disabled?: boolean;
  disabledReason?: string;
  /** Fires when the server accepts or rejects the current expression, so the
   *  owning dialog can keep its submit button in step with the inline error. */
  onValidityChange?: (valid: boolean) => void;
}

const PREVIEW_DEBOUNCE_MS = 300;

type EveryPattern = Extract<TimePattern, { kind: "every" }>;
type ScheduleWindow = { from: string; to: string };

function useNowTicker(intervalMs = 30_000): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

function useFormatCountdown() {
  const { t } = useT("autopilots");
  return (target: Date, now: Date): string => {
    const diffMs = target.getTime() - now.getTime();
    if (diffMs < 60_000) return t(($) => $.schedule_editor.countdown.less_than_minute);
    const minutes = Math.floor(diffMs / 60_000);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    if (days > 0)
      return t(($) => $.schedule_editor.countdown.days_hours, {
        days,
        hours: hours % 24,
        minutes: minutes % 60,
      });
    if (hours > 0)
      return t(($) => $.schedule_editor.countdown.hours_minutes, {
        hours,
        minutes: minutes % 60,
      });
    return t(($) => $.schedule_editor.countdown.minutes, { minutes });
  };
}

// The window is a dimension of the schedule, not a mode of the editor: all day is
// the window that spans the day, and the model spells that one way — `window:
// null`. Every window the editor commits goes through here, so the two never drift
// into two representations of the same schedule.
//
// The bounds are read as hours. A minute-step window is hour-granular (its ends
// read :00 and :59), and an hours-step window carries the firing minute at both
// ends — neither minute has a say in whether the window spans the day.
function isFullDay(window: ScheduleWindow): boolean {
  return timeParts(window.from).hour === 0 && timeParts(window.to).hour === 23;
}

/** The window as the controls show it. All day has no window of its own, and the
 *  fields still have to render something: the bounds it stands for. */
function displayWindow(time: EveryPattern): ScheduleWindow {
  if (time.window !== null) return time.window;
  return time.unit === "hours"
    ? { from: `00:${pad2(time.minute)}`, to: `23:${pad2(time.minute)}` }
    : { from: "00:00", to: "23:59" };
}

// An "every N hours, all day" pattern has nowhere to keep an hour, so the last
// hour:minute the user actually expressed is carried outside the model. Without
// it, at 14:30 → interval → at silently comes back as 09:30.
//
// The hour comes from the window, the minute from `minute` — never from the
// window's own text. A minute-step window is hour-granular, so its bounds read
// :00; taking the minute from there would drop the 30 of a 14:30 the user typed
// before switching, which the model still holds.
//
// All day has no hour to give: the 00:00 its fields show is the absence of a
// bound, not a time the user picked, and carrying it over would rewrite their
// 14:30 to 00:30 on the way back to a fixed time.
function timeAnchorOf(time: TimePattern): string | null {
  if (time.kind === "at") return time.time;
  if (time.window === null) return null;
  return `${pad2(timeParts(time.window.from).hour)}:${pad2(time.minute)}`;
}

function defaultAtTime(prev: TimePattern, anchor: string): string {
  return (
    timeAnchorOf(prev) ??
    `${pad2(timeParts(anchor).hour)}:${pad2(prev.kind === "every" ? prev.minute : 0)}`
  );
}

// Switching to a fixed time can only carry one HH:MM, so the step, the unit and
// the window would be gone on the way back — "every 3h, 9:00–21:00" would return
// as "every hour, all day". They are remembered outside the model and restored,
// rebased on whatever time the user is now switching away from.
function defaultEveryTime(prev: TimePattern, anchor: EveryPattern | null): EveryPattern {
  if (prev.kind === "every") return prev;
  const { hour, minute } = timeParts(prev.time);
  const base: EveryPattern = anchor ?? {
    kind: "every",
    interval: 1,
    unit: "hours",
    window: null,
    minute,
  };
  const time: EveryPattern = { ...base, minute };
  if (time.window === null) return time;
  // The window restarts at the time the user is switching away from — but only
  // where that still leaves a window. Rebasing "09:00–15:00" onto a 22:00 fixed
  // time would drag the end up to 22 and hand back the single hour 22:00–22:00,
  // destroying the very window this anchor exists to keep.
  // Strictly earlier: a start rebased onto the end hour itself leaves the single
  // hour 15:00–15:00, which is the same collapse by another name.
  const rebased =
    hour < timeParts(time.window.to).hour
      ? { from: `${pad2(hour)}:${pad2(minute)}`, to: time.window.to }
      : time.window;
  // Rebasing a window that ends at 23 onto a midnight fixed time spans the whole
  // day, and a window that spans the day is not one — the model holds that as all
  // day, and nothing else may hand back a second form of it.
  const window = clampWindow(rebased, time.unit, minute);
  return { ...time, window: isFullDay(window) ? null : window };
}

// Keep windows canonical: `minute` — not the window's own current text — is the
// single source of truth for the firing minute, so a hours → minutes → hours
// unit round-trip restores it instead of leaving the zeroed value behind. Minute
// intervals are hour-granular, and `to` is never earlier than `from`.
function clampWindow(
  window: ScheduleWindow,
  unit: EveryPattern["unit"],
  minute: number,
): ScheduleWindow {
  const from = timeParts(window.from);
  const to = timeParts(window.to);
  const edgeMinute = unit === "hours" ? minute : 0;
  const endMinute = unit === "hours" ? minute : 59;
  const endHour = Math.max(to.hour, from.hour);
  return {
    from: `${pad2(from.hour)}:${pad2(edgeMinute)}`,
    to: `${pad2(endHour)}:${pad2(endMinute)}`,
  };
}

function toggleDay(days: number[], day: number): number[] {
  if (days.includes(day)) {
    if (days.length === 1) return days;
    return days.filter((d) => d !== day);
  }
  return [...days, day].toSorted((a, b) => a - b);
}

/** One dimension of the schedule. A fieldset, not a styled div: a greyed-out
 *  control that keyboard users can still reach would silently overwrite the
 *  stored expression. */
function ScheduleField({
  label,
  disabled,
  children,
}: {
  label: string;
  disabled: boolean;
  children: ReactNode;
}) {
  return (
    // No `m-0` here: it ties with the parent's space-y on specificity and wins on
    // source order, zeroing the gap between the blocks. Preflight already resets
    // the fieldset's margin.
    //
    // Label above its controls, each block full width: the panel is a narrow
    // sidebar column, and a label column would take a quarter of it from the
    // controls that have to hold a whole time range.
    // 6px label→control, against the 8px between a block's own controls and the
    // 20px between blocks, so the label reads as the heading of what follows it.
    <fieldset
      disabled={disabled}
      className={cn("flex min-w-0 flex-col gap-1.5 border-0 p-0", disabled && "opacity-60")}
    >
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      {/* flex gap, not space-y: Base UI's Select appends a hidden fixed-position
          form input after the trigger, which space-y counts as the last child —
          handing its 8px to the visible control and inflating the block. */}
      <div className="flex min-w-0 flex-col gap-2">{children}</div>
    </fieldset>
  );
}

/** Number field whose DOM text and model value cannot drift apart: clearing it
 *  is allowed while typing, and blur reconciles the box with the model —
 *  clamping an out-of-range number into the field's bounds, and snapping back to
 *  the committed value when the box holds no number at all. Without the clamp,
 *  typing "24" into a 1–23 field would commit the 2 of the first keystroke and
 *  then reject the pair, leaving "every 2 hours" — a schedule the user never
 *  chose — behind. */
function NumberField({
  value,
  min,
  max,
  onCommit,
  className,
  ariaLabel,
  autoFocus,
  // The box this field is drawn in. Standing alone it brings its own; inside an
  // InputGroup the group owns the border, the ring and the focus slot, and
  // InputGroupInput is the same field with all three given up.
  component: Field = Input,
}: {
  value: number;
  min: number;
  max: number;
  onCommit: (value: number) => void;
  className?: string;
  ariaLabel: string;
  autoFocus?: boolean;
  component?: ComponentType<ComponentProps<"input">>;
}) {
  const [text, setText] = useState(String(value));
  const lastValueRef = useRef(value);
  if (lastValueRef.current !== value) {
    lastValueRef.current = value;
    setText(String(value));
  }
  return (
    <Field
      type="number"
      aria-label={ariaLabel}
      // Caller-gated: set only when the user just revealed this field, never on
      // first mount.
      autoFocus={autoFocus}
      min={min}
      max={max}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
        const n = parseInt(e.target.value, 10);
        if (!Number.isNaN(n) && n >= min && n <= max) onCommit(n);
      }}
      // The native stepper stops dead at the bounds; every other field in this
      // panel wraps, so these do too. Stepping off 31 gives day 1 back, not
      // another press that does nothing.
      onKeyDown={(e) => {
        if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
        e.preventDefault();
        const typed = parseInt(text, 10);
        // An out-of-range box is not somewhere to step FROM — stepping is what
        // brings it back in, and it lands on the bound it overshot. Clamping
        // first and then stepping would step off that bound instead, so the
        // bound itself could never be reached: in a 1-31 field, an arrow on a
        // typed "0" gave 2, and one on "40" gave 30.
        const stepped =
          Number.isNaN(typed) || typed >= min && typed <= max
            ? (Number.isNaN(typed) ? value : typed) + (e.key === "ArrowUp" ? 1 : -1)
            : Math.min(max, Math.max(min, typed));
        const wrapped = stepped > max ? min : stepped < min ? max : stepped;
        setText(String(wrapped));
        if (wrapped !== lastValueRef.current) onCommit(wrapped);
      }}
      onBlur={() => {
        const n = parseInt(text, 10);
        if (Number.isNaN(n)) {
          setText(String(lastValueRef.current));
          return;
        }
        const clamped = Math.min(max, Math.max(min, n));
        setText(String(clamped));
        if (clamped !== lastValueRef.current) onCommit(clamped);
      }}
      className={className}
    />
  );
}

// The two window fields sit tight against each other so the interval box beside
// them keeps the width. Trimmed from the field's default padding and digit gap:
// the group's own border already frames them, and there is nothing between the
// two ends but the "~", so the roomier standalone spacing is width the row can't
// spare here.
const WINDOW_FIELD_COMPACT = "gap-0.5 px-1";

export function ScheduleEditor({
  value,
  onChange,
  wsId,
  disabled,
  disabledReason,
  onValidityChange,
}: ScheduleEditorProps) {
  const { t, i18n } = useT("autopilots");
  const describe = useDescribeSchedule();
  const formatCountdown = useFormatCountdown();
  const now = useNowTicker();
  const advanced = value.raw !== null;
  const locked = disabled === true;

  // The cron text mirrors the structured model except while the user is
  // typing in the cron input, where the draft wins until it is applied.
  // Applying only on blur/Enter (never on a typing pause) keeps a half-typed
  // expression from flipping the editor into advanced-only mode mid-edit.
  const [cronDraft, setCronDraft] = useState<string | null>(null);
  // Two views of the committed expression. The wire form (toCron) carries the
  // timezone as a `TZ=` prefix and is what validation and saving see; the box
  // holds only the five fields, because its prefix is a fixed group segment
  // drawn from the picker, not characters the user can edit or delete.
  const committedCron = toCron(value);
  const fieldsText = cronFields(value);
  const cronText = cronDraft ?? fieldsText;
  // A typed `TZ=` prefix lives in the text itself until the server has ruled
  // on it (see applyDraft), and while it does the fixed segment yields — two
  // prefixes on screen would read as an expression that carries both.
  const ownPrefix = hasTimezonePrefix(cronText);
  const [cronEditing, setCronEditing] = useState(false);
  const cronInputRef = useRef<HTMLInputElement | null>(null);
  // Autofocus leaves the caret at the end, scrolling a long expression's head
  // out of view the moment it opens; the edit starts where the readback left off.
  useEffect(() => {
    if (!cronEditing) return;
    const input = cronInputRef.current;
    if (input === null) return;
    input.setSelectionRange(0, 0);
    input.scrollLeft = 0;
  }, [cronEditing]);
  const cronErrorId = useId();
  // Advanced-only expressions cannot be edited anywhere else, so their field is
  // always open.
  const cronOpen = advanced || cronEditing;

  const timeAnchorRef = useRef(timeAnchorOf(value.time) ?? "09:00");
  const currentAnchor = timeAnchorOf(value.time);
  if (currentAnchor !== null) timeAnchorRef.current = currentAnchor;

  const everyAnchorRef = useRef<EveryPattern | null>(null);
  if (value.time.kind === "every") everyAnchorRef.current = value.time;

  // Hours cap the step at 23, minutes at 59, so switching units clamps — and a
  // step of 45 minutes would come back from a glance at "hours" as 23. Each
  // unit remembers its own last step.
  const intervalAnchorRef = useRef<Record<EveryPattern["unit"], number | null>>({
    hours: null,
    minutes: null,
  });
  if (value.time.kind === "every") {
    intervalAnchorRef.current[value.time.unit] = value.time.interval;
  }

  // The day kinds cannot hold each other's value either: "every day" has no day
  // set, weekly has no day number. A glance at another kind and back would
  // otherwise return Tue/Thu/Sat as Monday, and day 25 as day 1.
  const daysOfWeekAnchorRef = useRef<number[]>([1]);
  const dayOfMonthAnchorRef = useRef(1);
  if (value.days.kind === "weekly") daysOfWeekAnchorRef.current = value.days.daysOfWeek;
  if (value.days.kind === "monthly") dayOfMonthAnchorRef.current = value.days.dayOfMonth;

  // Focus the field a mode switch just revealed, so the user can type straight
  // away: the time toggle swaps in a fresh control, and picking "Day of month"
  // brings up the day box. The flag is set at the switch, read by the mounting
  // control's autoFocus, and cleared after the commit — so it fires only on a
  // user switch, never on the dialog's first render (which would steal focus and
  // scroll the panel on open). A passive effect resets it, well after autoFocus
  // has run during the commit.
  const focusTimeFieldRef = useRef(false);
  const focusDayFieldRef = useRef(false);
  useEffect(() => {
    focusTimeFieldRef.current = false;
    focusDayFieldRef.current = false;
  });

  // A typed expression carrying a `TZ=` prefix, awaiting the server's verdict
  // before it is promoted to its extracted form (see applyDraft). Compared
  // against committedCron before every use, so a later edit retires it without
  // needing to be cleared.
  const pendingTzPromotionRef = useRef<string | null>(null);

  /** Commit the cron box's text. Returns whether the editor is in advanced-only
   *  mode afterwards — the caller cannot read that off `advanced`, which still
   *  holds the value from the render being left. */
  const applyDraft = (draft: string): boolean => {
    const next = draft.trim();
    setCronDraft(null);
    // An empty expression is not a schedule: snap back to the model rather
    // than writing "" into `raw` and letting the dialog submit it.
    if (next.length === 0 || next === cronFields(value)) return advanced;
    // A typed `TZ=` prefix is NOT extracted here. Extraction rewrites two
    // controls at once — the zone lands in the picker, the text loses the
    // prefix — and only the server can say the zone is real, so both rewrites
    // wait for its verdict: the expression is committed verbatim (advanced),
    // and the effect below promotes it to the extracted pair when the preview
    // for this exact text comes back accepted. Until then the picker never
    // shows a zone the server might reject, and the box never loses a
    // character the user could still be fixing. (Hydration is different:
    // parseCron extracts immediately there, because a STORED expression
    // already passed the server's validation when it was saved.)
    if (extractTimezonePrefix(next) !== null) {
      pendingTzPromotionRef.current = next;
      onChange({ ...value, raw: next });
      return true;
    }
    pendingTzPromotionRef.current = null;
    const parsed = parseCron(next, value.timezone);
    // An expression beyond the model keeps the structured schedule underneath
    // it. parseCron has no previous config to work from and hands back the
    // 09:00 defaults with the text in `raw`; letting those defaults land would
    // show the greyed-out controls a schedule the user never had, and lose
    // theirs the moment they simplify the expression again.
    onChange(parsed.raw === null ? parsed : { ...value, raw: next });
    return parsed.raw !== null;
  };

  const setTime = (time: TimePattern) => {
    setCronDraft(null);
    onChange({ ...value, time, raw: null });
  };
  const setDays = (days: DayPattern) => {
    setCronDraft(null);
    onChange({ ...value, days, raw: null });
  };
  // Timezone is orthogonal to the expression, so it stays editable — and
  // `raw` stays untouched — in advanced-only mode.
  const setTimezone = (timezone: string) => onChange({ ...value, timezone });

  // The preview stays on even when the editor is locked: it is read-only
  // information the user still needs (the old editor showed it too). It follows
  // the committed expression, not the draft — validation happens once the cron
  // field is left, never on every keystroke.
  const previewExpr = useDebouncedValue(committedCron, PREVIEW_DEBOUNCE_MS);
  const preview = useQuery(
    cronPreviewOptions(wsId, previewExpr, value.timezone, {
      enabled: previewExpr.trim().length > 0,
    }),
  );
  const { refetch } = preview;
  // `null` means the response could not be read (schema drift) — distinct from
  // an empty list, which means the expression genuinely never fires.
  const nextRuns = preview.data?.next_runs ?? null;

  // Only a 400 means "this input is invalid" — a transport or server failure
  // must not be painted as the user's cron being wrong. The server tags which
  // input it rejected, because the cron and the timezone are fixed in
  // different controls.
  const previewIsCurrent = previewExpr === committedCron;
  const liveRejection =
    previewIsCurrent && preview.error instanceof ApiError && preview.error.status === 400
      ? preview.error
      : null;

  // Promote a pending `TZ=` expression once the server has accepted it — and
  // only the exact text the verdict is about. The preview judged the verbatim
  // prefixed expression (the server reads the embedded zone itself), so a 200
  // covers both rewrites the promotion makes: the zone moves into the picker
  // and the prefix comes off the cron box. On a rejection nothing happens: the
  // text stays as typed, under the server's own error.
  const promoted = pendingTzPromotionRef.current;
  useEffect(() => {
    if (promoted === null || promoted !== committedCron) return;
    if (!previewIsCurrent || !preview.isSuccess) return;
    pendingTzPromotionRef.current = null;
    onChange(parseCron(promoted, value.timezone));
  }, [promoted, committedCron, previewIsCurrent, preview.isSuccess, value.timezone, onChange]);
  // The server's last verdict outlives the request that produced it, but only
  // for the exact input it judged — the expression AND the timezone, since the
  // server rejects either. While that input is re-queried (a retry, a refetch
  // after an outage) the query holds neither error nor data, and without this a
  // rejection would blink out and Save would go live for a cron the server has
  // already refused — which the pre-write check cannot catch either, since it
  // passes a schedule through when the network is what failed. Only a readable
  // answer changes the verdict; a transport failure says nothing about the cron,
  // and a change to either input retires it by no longer matching the key.
  //
  // Two keys live in this component, one debounce apart. `committedKey` is the
  // input the user has settled on; `queriedKey` (below) is what the query — and
  // so its `refetch` and the data in hand — actually belongs to. Anything
  // reasoning about the user's input takes the committed one; anything reasoning
  // about the request takes the queried one.
  const committedKey = `${committedCron} ${value.timezone}`;
  const verdictRef = useRef<{
    key: string;
    rejection: ApiError | null;
    accepted: boolean;
  } | null>(null);
  if (previewIsCurrent) {
    if (liveRejection !== null) {
      verdictRef.current = { key: committedKey, rejection: liveRejection, accepted: false };
    } else if (preview.isSuccess) {
      // `accepted` licenses the advanced notice: "the server accepts this
      // expression", not "has not rejected it". An unreadable 200 (schema
      // drift) accepts nothing.
      verdictRef.current = { key: committedKey, rejection: null, accepted: nextRuns !== null };
    }
  }
  const verdict = verdictRef.current?.key === committedKey ? verdictRef.current : null;
  const rejection = liveRejection ?? verdict?.rejection ?? null;
  const scheduleRejection = rejection !== null ? classifyScheduleRejection(rejection) : null;
  const cronErrorDetail = scheduleRejection?.detail ?? null;
  const previewUnavailable =
    previewIsCurrent &&
    ((preview.error !== null && cronErrorDetail === null) ||
      (preview.isSuccess && nextRuns === null));

  // The preview arrives one round trip after every edit. Unmounting the line
  // while it is in flight makes it blink out and back on each keystroke, so the
  // last answer stays on screen and its text is replaced in place. It is dimmed
  // and marked busy until it belongs to the current expression, so a stale list
  // is never presented as this schedule's answer.
  const previewIsSettled = previewIsCurrent && preview.isSuccess && nextRuns !== null;
  const serverAccepted = verdict?.accepted === true;
  const shownPreviewRef = useRef<{ runs: string[]; timezone: string } | null>(null);
  if (
    previewIsSettled &&
    (shownPreviewRef.current?.runs !== nextRuns ||
      shownPreviewRef.current.timezone !== value.timezone)
  ) {
    shownPreviewRef.current = { runs: nextRuns, timezone: value.timezone };
  }
  const shownPreview = shownPreviewRef.current;
  // Formatted once per preview, not per render: every keystroke in the cron box
  // and every 30s countdown tick re-renders the editor, while these Intl formats
  // only change when the preview or locale does. Each run renders in full —
  // "Jul 13, 3:00 PM", "7月13日 15:00" — because a bare time under a dated line
  // would read as a footnote to it.
  const shownRuns = useMemo(() => {
    if (shownPreview === null) return [];
    return shownPreview.runs.map((iso) => ({
      iso,
      label: formatInTimeZone(iso, shownPreview.timezone, i18n.language),
      at: Date.parse(iso),
    }));
  }, [shownPreview, i18n.language]);
  const previewIsPending = !previewIsSettled;
  // Busy/dimming only makes sense over the list: the unavailable message never
  // settles, and dimming it forever would read as a permanent loading state.
  const previewShowsList =
    cronErrorDetail === null &&
    !previewUnavailable &&
    shownPreview !== null &&
    shownPreview.runs.length > 0;

  // A malformed timestamp must degrade to "no countdown", never to NaN, so every
  // value is checked before it becomes a Date (see the list below).
  const firstRunMs = nextRuns?.[0] !== undefined ? Date.parse(nextRuns[0]) : Number.NaN;

  // The preview is cached, so once its first entry is in the past the whole
  // list is stale — refresh it. The guard names the input AND the run it was
  // fired for: without the run, a client clock ahead of the server would refetch
  // on every tick; without the input, two schedules sharing a next-run instant
  // (any two that fire at tomorrow 09:00 do) would take one's refresh as
  // covering the other, and nothing else would rescue it — the preview is fresh
  // for 30s and refetchOnWindowFocus is off repo-wide. The input is the
  // *debounced* expression, not the committed one: the query, `refetch` and
  // `firstRunMs` all belong to that, and naming the committed one would mark the
  // new expression as refreshed while the data in hand is still the previous
  // expression's, swallowing the refresh it is owed once its preview arrives.
  const queriedKey = `${previewExpr} ${value.timezone}`;
  const refetchedForRef = useRef<string | null>(null);
  useEffect(() => {
    if (Number.isNaN(firstRunMs) || firstRunMs > now.getTime()) return;
    const fetchedFor = `${queriedKey} ${firstRunMs}`;
    if (refetchedForRef.current === fetchedFor) return;
    refetchedForRef.current = fetchedFor;
    void refetch();
  }, [firstRunMs, now, refetch, queriedKey]);

  // The dialog owns the submit button, so it needs to know the expression the
  // server rejected — otherwise Save persists a cron that can never run. The
  // expression and the timezone are deps of their own: a dialog that optimistically
  // cleared its own submit-time rejection needs the verdict restated even when the
  // rejection text is the one it already had (a cached 400 for the query this
  // edit lands on arrives with no null in between).
  useEffect(() => {
    onValidityChange?.(cronErrorDetail === null);
  }, [cronErrorDetail, committedCron, value.timezone, onValidityChange]);

  const timezones = useMemo(() => timezoneOptions(value.timezone), [value.timezone]);

  const description = describe(value);

  const dayKindLabel = (kind: DayPattern["kind"]): string => {
    if (kind === "every") return t(($) => $.schedule_editor.days_every);
    if (kind === "weekly") return t(($) => $.schedule_editor.days_weekly);
    return t(($) => $.schedule_editor.days_monthly);
  };
  const dayKindItems = (["every", "weekly", "monthly"] as const).map((kind) => ({
    value: kind,
    label: dayKindLabel(kind),
  }));

  return (
    // 20px between blocks, against 8px inside one (label→control, control→its
    // own extra rows): a block must read as further from its neighbour than from
    // its own parts, or the whole panel collapses into one wall of controls.
    <div className="space-y-5">
      <ScheduleField label={t(($) => $.schedule_editor.time_label)} disabled={locked || advanced}>
        <SegmentedToggle
          value={value.time.kind}
          options={[
            ["at", t(($) => $.schedule_editor.time_at)],
            ["every", t(($) => $.schedule_editor.time_every)],
          ]}
          onChange={(kind) => {
            focusTimeFieldRef.current = true;
            setTime(
              kind === "at"
                ? {
                    kind: "at",
                    time: defaultAtTime(value.time, timeAnchorRef.current),
                  }
                : defaultEveryTime(value.time, everyAnchorRef.current),
            );
          }}
        />
        {value.time.kind === "at" ? (
          <TimeInput
            value={value.time.time}
            hourLabel={t(($) => $.schedule_editor.a11y.fixed_hour)}
            minuteLabel={t(($) => $.schedule_editor.a11y.fixed_minute)}
            onChange={(v) => setTime({ kind: "at", time: v })}
            autoFocus={focusTimeFieldRef.current}
          />
        ) : (
          <EveryTimeControls
            time={value.time}
            intervalAnchor={intervalAnchorRef.current}
            onSet={setTime}
            autoFocus={focusTimeFieldRef.current}
          />
        )}
      </ScheduleField>

      <ScheduleField label={t(($) => $.schedule_editor.days_label)} disabled={locked || advanced}>
        {/* A 7:4 grid, the same template as the interval row above, so the two
            rows line up as one two-column grid at the minutes unit. Grid tracks
            size by the template alone — unlike flex, which let each box's own
            padding and w-fit/w-full skew the split and knocked the columns out of
            line. When there is no day-of-month box the select spans both tracks,
            so every/weekly still fill the width. */}
        <div className="grid grid-cols-[7fr_4fr] items-center gap-2">
          <Select
            items={dayKindItems}
            value={value.days.kind}
            onValueChange={(v) => {
              if (!v || v === value.days.kind) return;
              if (v === "every") setDays({ kind: "every" });
              else if (v === "weekly")
                setDays({ kind: "weekly", daysOfWeek: daysOfWeekAnchorRef.current });
              else {
                // Only monthly reveals a field to type into; the day box takes the
                // caret so the day can be set without a second click.
                focusDayFieldRef.current = true;
                setDays({ kind: "monthly", dayOfMonth: dayOfMonthAnchorRef.current });
              }
            }}
          >
            <SelectTrigger
              aria-label={t(($) => $.schedule_editor.a11y.day_pattern)}
              className={cn("w-full min-w-0", value.days.kind !== "monthly" && "col-span-2")}
            >
              <SelectValue>{dayKindLabel(value.days.kind)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {dayKindItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {value.days.kind === "monthly" && (
            <NumberField
              value={value.days.dayOfMonth}
              min={1}
              max={31}
              ariaLabel={t(($) => $.schedule_editor.a11y.day_of_month)}
              onCommit={(n) => setDays({ kind: "monthly", dayOfMonth: n })}
              className="h-8 min-w-0"
              autoFocus={focusDayFieldRef.current}
            />
          )}
        </div>
        {value.days.kind === "weekly" && (
          <div className="flex w-full gap-0.5">
            {DAY_KEYS.map((dayKey, i) => {
              const days = value.days;
              const selected = days.kind === "weekly" && days.daysOfWeek.includes(i);
              return (
                <button
                  key={dayKey}
                  type="button"
                  aria-pressed={selected}
                  // The chip's own text is an abbreviation; the name is not.
                  aria-label={t(($) => $.schedule_editor.a11y.days_full[dayKey])}
                  onClick={() => {
                    if (days.kind !== "weekly") return;
                    setDays({
                      kind: "weekly",
                      daysOfWeek: toggleDay(days.daysOfWeek, i),
                    });
                  }}
                  // flex-1 + min-w-0: the seven chips split the row evenly, so
                  // both states share one box model and label width can't
                  // change a chip's size.
                  className={cn(
                    "inline-flex h-6.5 min-w-0 flex-1 items-center justify-center rounded-md",
                    "text-[11px] font-medium leading-none transition-colors",
                    selected
                      ? "bg-foreground text-background"
                      : "bg-muted text-muted-foreground hover:text-foreground",
                  )}
                >
                  {t(($) => $.schedule_editor.days_short[dayKey])}
                </button>
              );
            })}
          </div>
        )}
        {value.days.kind === "monthly" && value.days.dayOfMonth >= 29 && (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.schedule_editor.monthly_short_month_hint, {
              day: value.days.dayOfMonth,
            })}
          </p>
        )}
      </ScheduleField>

      <ScheduleField label={t(($) => $.schedule_editor.timezone_label)} disabled={locked}>
        {/* The fieldset already disables the trigger; disabled:opacity-100 keeps
            the control from dimming a second time on top of it. */}
        <TimezonePicker
          value={value.timezone}
          onChange={setTimezone}
          options={timezones}
          ariaLabel={t(($) => $.schedule_editor.a11y.timezone)}
          className="disabled:opacity-100"
        />
      </ScheduleField>

      {/* The cron text, the plain-language readback and the server preview are
          all derived views of the same expression, so they share one panel: the
          result of the fields above, not more fields alongside them. The cron
          line inside it doubles as the advanced editing entry — clicking it
          swaps the text for an input, and the form dims while raw mode holds. */}
      <div className="rounded-md bg-muted/40 p-2.5 text-xs text-muted-foreground">
        <div className="space-y-2">
          {/* The plain-language sentence leads: it is the line a person reads,
              so it takes the panel's entry and the foreground color, and the
              expression drops to a technical echo below it. */}
          {description !== null && (
            <p className="flex items-start gap-1.5 text-foreground">
              <Clock className="mt-0.5 h-3 w-3 shrink-0" />
              <span>{description}</span>
            </p>
          )}
          {cronOpen ? (
            // The `TZ=` segment is part of the expression but not of the editable
            // text: it renders as a fixed addon drawn from the timezone picker, so
            // the zone cannot be half-deleted into a prefix robfig would panic on,
            // and the fields flow to the server exactly as typed.
            <InputGroup className="bg-background dark:bg-input/30">
              {!ownPrefix && (
                // Its own row above the fields, mirroring the closed readback:
                // a long zone name and long fields truncate independently
                // instead of splitting one line between them.
                <InputGroupAddon align="block-start" className="font-mono text-xs">
                  {/* eslint-disable-next-line i18next/no-literal-string -- cron syntax, not copy */}
                  <span className="min-w-0 truncate">TZ={value.timezone}</span>
                </InputGroupAddon>
              )}
              <InputGroupInput
                ref={cronInputRef}
                type="text"
                // Only when the user opened the field. An advanced-only expression
                // has it open from the first render, and autofocusing there would
                // pull focus (and the dialog's scroll) to the bottom of the panel
                // on open, past the title the user came to edit.
                autoFocus={cronEditing}
                aria-label={t(($) => $.schedule_editor.cron_toggle)}
                value={cronText}
                disabled={locked}
                onChange={(e) => setCronDraft(e.target.value)}
                onBlur={() => {
                  // The mode after the draft lands, not the one being left: an
                  // expression simplified back into the model must close the field,
                  // and `advanced` still reads true in this render.
                  const stillAdvanced = cronDraft !== null ? applyDraft(cronDraft) : advanced;
                  // Advanced-only expressions have no structured view to fall back
                  // to, so the field stays open.
                  if (!stillAdvanced) setCronEditing(false);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && cronDraft !== null) {
                    e.preventDefault();
                    // Applying can end advanced-only mode, which would collapse the
                    // field back to its readback and drop focus to the body under
                    // the user's hands. The field stays open until they leave it.
                    setCronEditing(true);
                    applyDraft(cronDraft);
                  }
                }}
                aria-invalid={cronErrorDetail !== null}
                aria-describedby={cronErrorDetail !== null ? cronErrorId : undefined}
                className="font-mono text-sm"
              />
            </InputGroup>
          ) : (
            // The expression is edited where it is shown — clicking it swaps the
            // text for the field, instead of opening a second copy underneath.
            <button
              type="button"
              disabled={locked}
              onClick={() => setCronEditing(true)}
              // The pencil is decoration; this is the name a screen reader hears.
              aria-label={t(($) => $.schedule_editor.cron_click_to_edit)}
              className="flex w-full items-center gap-1.5 text-left hover:text-foreground disabled:pointer-events-none disabled:opacity-60"
            >
              {/* Closed, the readback shows only the editable text — the zone
                  is already on screen in the picker above, and a self-prefixed
                  advanced expression carries its zone in the text itself. The
                  full wire form appears when the field opens, as the fixed
                  segment above the fields. */}
              <span className="min-w-0 truncate font-mono">{cronText}</span>
              <Pencil aria-hidden className="h-3 w-3 shrink-0 opacity-60" />
            </button>
          )}
          {/* One note under the expression, never a stack of them: a rejection is
              what the user must act on, the advanced notice explains why the
              controls are off, and the syntax hint is the fallback. */}
          {cronErrorDetail !== null ? (
            // The rejection names the field it belongs to (aria-describedby) and
            // announces itself: without it, the only feedback a screen-reader
            // user gets for a bad expression is a Save button that went quiet.
            <div id={cronErrorId} role="alert" className="space-y-0.5">
              <p className="text-destructive">
                {scheduleRejection?.code === "invalid_timezone"
                  ? t(($) => $.schedule_editor.timezone_invalid)
                  : t(($) => $.schedule_editor.cron_invalid)}
              </p>
              {/* The parser's own words, verbatim — untranslated, but it is the
                  only text that says which field is wrong. */}
              <p className="font-mono text-[11px] text-destructive/70">{cronErrorDetail}</p>
            </div>
          ) : advanced ? (
            // Three different things are being said here, and only the first is
            // a statement about the expression: the server took it and the
            // controls cannot show it; we could not ask; we have not asked yet.
            <p>
              {serverAccepted
                ? t(($) => $.schedule_editor.advanced_hint)
                : previewUnavailable
                  ? t(($) => $.schedule_editor.advanced_unverified)
                  : t(($) => $.schedule_editor.advanced_checking)}
            </p>
          ) : cronOpen ? (
            <p>{t(($) => $.schedule_editor.cron_hint)}</p>
          ) : null}
        </div>

        {/* A rejected expression has nothing to preview: the whole section goes,
            heading and divider with it, rather than leaving an empty frame under
            the error. The error already fills the space it vacates.

            Otherwise the height is reserved: the preview is a round trip behind
            every edit, and a section that collapsed and reappeared would reflow
            the dialog. Same elements across fetches: React swaps the text nodes
            instead of tearing the section down, so a re-render of the same
            schedule never flashes — it just dims and reports busy until the
            answer is current. */}
        {cronErrorDetail === null && (
        <div
          aria-busy={previewShowsList ? previewIsPending : undefined}
          className={cn(
            "mt-2.5 min-h-14 border-t border-border/60 pt-2.5 transition-opacity",
            previewShowsList && previewIsPending && "opacity-50",
          )}
        >
          <p className="mb-1.5 font-medium text-foreground">
            {t(($) => $.schedule_editor.next_runs_label)}
          </p>
          {previewUnavailable ? (
            <p>{t(($) => $.schedule_editor.preview_unavailable)}</p>
          ) : shownPreview !== null && shownPreview.runs.length > 0 ? (
            // A grid, not per-row flex: the localized dates are not fixed-width,
            // so only a shared column can line the countdowns up with each
            // other. max-content sizes that column to the widest date.
            <ul className="grid grid-cols-[max-content_max-content] gap-x-5 gap-y-1">
              {shownRuns.map(({ iso, label, at }) => (
                // contents: the row's two cells belong to the grid above,
                // not to a box of their own.
                <li key={iso} className="contents">
                  {/* Dark date, dim countdown: the icon column is gone — the
                      grid already lines the rows up, and the describe line's
                      clock stays the panel's only icon. */}
                  <span className="text-foreground tabular-nums">{label}</span>
                  {/* Each run carries its own countdown, next to the time
                      it counts down to. */}
                  <span className="whitespace-nowrap tabular-nums opacity-70">
                    {Number.isNaN(at)
                      ? ""
                      : t(($) => $.schedule_editor.next_in, {
                          countdown: formatCountdown(new Date(at), now),
                        })}
                  </span>
                </li>
              ))}
            </ul>
          ) : previewIsSettled ? (
            // An empty list from a readable response: valid syntax, never fires.
            <p>{t(($) => $.schedule_editor.no_upcoming_runs)}</p>
          ) : null}
        </div>
        )}
      </div>
      {disabled === true && disabledReason !== undefined && (
        <p className="mt-2 text-[11px] text-muted-foreground">{disabledReason}</p>
      )}
    </div>
  );
}

function EveryTimeControls({
  time,
  intervalAnchor,
  onSet,
  autoFocus,
}: {
  time: EveryPattern;
  intervalAnchor: Record<EveryPattern["unit"], number | null>;
  onSet: (time: TimePattern) => void;
  autoFocus?: boolean;
}) {
  const { t } = useT("autopilots");
  const maxInterval = time.unit === "hours" ? 23 : 59;
  const unitItems = (["hours", "minutes"] as const).map((unit) => ({
    value: unit,
    label:
      unit === "hours"
        ? t(($) => $.schedule_editor.unit_hours)
        : t(($) => $.schedule_editor.unit_minutes),
  }));
  // The bounds on screen. `time.window` is what the schedule holds — null while it
  // runs all day — and these are what the two fields show either way, so the user
  // narrows the window by editing the day's bounds rather than by first announcing
  // that they want a window at all.
  const window = displayWindow(time);

  // Focus the step field when the unit changes, so the user can retype it for the
  // new unit without reaching back. The field stays mounted across a unit switch,
  // so autoFocus (mount only) cannot do this — focus it once the switch has
  // re-rendered. Queried off the group rather than threaded through
  // NumberField/Input as a ref: those shared components do not forward one, and
  // the step is the group's only number input.
  const groupRef = useRef<HTMLDivElement>(null);
  const focusStepOnUnitChangeRef = useRef(false);
  useEffect(() => {
    if (!focusStepOnUnitChangeRef.current) return;
    focusStepOnUnitChangeRef.current = false;
    const step = groupRef.current?.querySelector<HTMLInputElement>('input[type="number"]');
    step?.focus();
    step?.select();
  }, [time.unit]);

  // The end the user actually asked for. A window cannot run backwards, so a
  // start pushed past the end drags the end along with it — but the end must
  // come back when the start does, or "09:00–15:00", briefly mis-typed as a
  // 22:00 start, is saved as "09:00–22:00" and the autopilot runs twice as often
  // as it was set to.
  const endAnchorRef = useRef<number | null>(null);
  // The end hour this control itself produced by dragging the start. Every other
  // end — typed into the end field or the cron box, hydrated from the server,
  // widened back out by hand — is the user's new intent and re-seeds the
  // anchor; only a drag leaves it standing, because a drag is what the anchor
  // exists to undo.
  //
  // Hours, not the "HH:MM" text: a unit switch rewrites 22:00 to 22:59 without
  // the user touching anything, and taking that for new intent would hand the
  // dragged-open window back as if they had asked for it.
  const draggedEndRef = useRef<number | null>(null);
  if (timeParts(window.to).hour !== draggedEndRef.current) {
    endAnchorRef.current = timeParts(window.to).hour;
  }
  const anchoredEnd = (from: string, to: string): string => {
    const end = Math.max(endAnchorRef.current ?? timeParts(to).hour, timeParts(from).hour);
    return `${pad2(end)}:${pad2(timeParts(to).minute)}`;
  };

  // Every edit of the two fields lands here, and a window that spans the day is
  // committed as the absence of one, so a schedule that runs around the clock has
  // a single form no matter which way the user arrived at it.
  const setWindow = (next: ScheduleWindow, minute: number) => {
    const clamped = clampWindow(next, time.unit, minute);
    onSet({ ...time, minute, window: isFullDay(clamped) ? null : clamped });
  };

  return (
    // Frequency and window on one line: "every N hours" is how often, "from~to"
    // is the daily span it runs within — two settings, two boxes, side by side.
    // A 7:4 grid, the same template as the day row below, so the two rows read as
    // one two-column grid at the minutes unit. The window's min-w-fit lets its
    // track grow past the 4-share to its content, so the wider HH:MM ~ HH:MM at
    // the hours unit fits (the interval track yields) instead of clipping — that
    // unit then steps out of the alignment, which is only asked of minutes.
    <div ref={groupRef} className="grid min-w-0 grid-cols-[7fr_4fr] items-center gap-2">
      {/* "Every 3 hours" is one setting, so it reads as one control: the prefix,
          the step and the unit share a single box. The step takes the room the
          other two leave — the prefix is as wide as its translation, the unit as
          wide as its longest option — so the field never has to be sized against
          text it cannot see. */}
      <InputGroup className="min-w-0">
        {/* pl-2.5, not the addon's default pl-2: this row sits directly above the
            day select, and the two read as one column only if their first
            character starts at the same x. A Select trigger pads to 2.5. */}
        <InputGroupAddon className="pl-2.5">
          {t(($) => $.schedule_editor.every_prefix)}
        </InputGroupAddon>
        <NumberField
          component={InputGroupInput}
          autoFocus={autoFocus}
          value={time.interval}
          min={1}
          max={maxInterval}
          ariaLabel={t(($) => $.schedule_editor.a11y.interval)}
          onCommit={(n) => onSet({ ...time, interval: n })}
          // The spinner would sit between the number and the unit, reading as a
          // control over the word beside it. The arrow keys still step, and still
          // wrap, which the spinner never did.
          // min-w floor so the flex-1 field can never be squeezed to nothing and
          // clip its digits when the row is tight; wide enough for the two-digit
          // max at either unit.
          className="min-w-[2.5ch] px-0 text-center [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
        />
        <Select
          items={unitItems}
          value={time.unit}
          onValueChange={(v) => {
            if (!v || v === time.unit) return;
            const unit = v as EveryPattern["unit"];
            const max = unit === "hours" ? 23 : 59;
            focusStepOnUnitChangeRef.current = true;
            onSet({
              ...time,
              unit,
              // The step this unit was last set to, if it ever was: clamping 45
              // minutes down to 23 hours is unavoidable, keeping the 23 on the
              // way back is not.
              interval: intervalAnchor[unit] ?? Math.min(time.interval, max),
              // `minute` is carried through untouched: zeroing it here would
              // silently drop the firing minute on a hours → minutes → hours
              // round-trip. toCron ignores it for minute patterns anyway.
              window: time.window === null ? null : clampWindow(time.window, unit, time.minute),
            });
          }}
        >
          <InputGroupSelectTrigger
            aria-label={t(($) => $.schedule_editor.a11y.interval_unit)}
            className="pr-2.5"
          >
            <SelectValue>
              {unitItems.find((item) => item.value === time.unit)?.label}
            </SelectValue>
          </InputGroupSelectTrigger>
          {/* min-w-[7rem]: below the component's 144px default, which the two
              short options don't need — but not so far that "minutes" and its
              check mark (the item's pr-8) lose room. */}
          <SelectContent className="min-w-[7rem]">
            {unitItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </InputGroup>
      {/* The window, always on screen. One clock leads the whole box (an addon,
          like the interval row's "Every"), not one per field: a single icon marks
          this as a time control without the width two would cost between the ends.

          A minute-step window is hour-granular — cron puts the step in the minute
          field, so the bounds carry no minute of their own. Both ends are the same
          in-place field either way; the minute segment is simply not offered where
          it would mean nothing. */}
      {/* Sits in the grid's 4-share second column, under the day row's number.
          min-w-fit lets that track grow past the share to the box's content: at
          the minutes unit the hour-only window fits the share and stays aligned;
          at hours the wider HH:MM ~ HH:MM pushes the track out and the interval
          yields, rather than the window clipping (its segments never shrink).
          has-disabled off: the group dims itself when anything inside it is
          disabled, and the minute segments are, by design, under a minute step —
          which does not make the row unavailable. A locked or advanced schedule
          still greys out, from the fieldset around the whole block. */}
      <InputGroup className="min-w-fit has-disabled:bg-transparent has-disabled:opacity-100 dark:has-disabled:bg-input/30">
        {/* Leads the box, not focusable: the fields after it take the focus and
            the group's border. pr-0 lets the start field's own padding set the
            icon-to-digit gap, so it matches the gap between the two ends. */}
        <InputGroupAddon className="pl-2 pr-0">
          <Clock className="size-3.5 text-muted-foreground" />
        </InputGroupAddon>
        <InputGroupTimeInput
          className={WINDOW_FIELD_COMPACT}
          showIcon={false}
          hourOnly={time.unit === "minutes"}
          hourLabel={t(($) => $.schedule_editor.a11y.window_start_hour)}
          minuteLabel={t(($) => $.schedule_editor.a11y.window_start_minute)}
          value={window.from}
          onChange={(v) => {
            // The window's two ends share one firing minute — cron has no
            // second minute to put there — so editing either end's minute
            // moves both. setWindow must get the minute just typed, not
            // time.minute: mid-two-digit-entry the model still holds the
            // first digit's value, which would overwrite the second one.
            const minute = time.unit === "hours" ? timeParts(v).minute : time.minute;
            const to = anchoredEnd(v, window.to);
            // Marks this end as the drag's doing, so the next render keeps the
            // anchor instead of taking the dragged value for a new intent.
            // anchoredEnd already keeps the hour at or above the start's, so
            // setWindow's clamp will not move it again.
            draggedEndRef.current = timeParts(to).hour;
            setWindow({ from: v, to }, minute);
          }}
        />
        <InputGroupText className="shrink-0">~</InputGroupText>
        {/* The window's end. The two ends constrain each other as they are edited
            — hourMin keeps the end from falling below the start — so a reversed
            window, which has no cron form, cannot even be displayed, let alone
            submitted. The bound is the field's own range rather than a clamp
            applied to whatever it emits: the arrow keys wrap inside it (stepping
            below the start hour lands on 23, not on a key that does nothing).

            Clearing it opens the window to the end of the day (hourClearTo)
            rather than collapsing it onto the start: backspace on the end of a
            window is how a keyboard user says "run until the day is out". */}
        <InputGroupTimeInput
          className={WINDOW_FIELD_COMPACT}
          showIcon={false}
          value={window.to}
          hourMin={timeParts(window.from).hour}
          hourClearTo={23}
          hourOnly={time.unit === "minutes"}
          hourLabel={t(($) => $.schedule_editor.a11y.window_end_hour)}
          minuteLabel={t(($) => $.schedule_editor.a11y.window_end_minute)}
          onChange={(v) => {
            const minute = time.unit === "hours" ? timeParts(v).minute : time.minute;
            // Typed, not dragged — the end the start has to give back. Set here
            // rather than left to the re-seeding rule, which cannot see intent
            // in an end that happens to equal the one a drag just produced.
            endAnchorRef.current = timeParts(v).hour;
            draggedEndRef.current = timeParts(v).hour;
            setWindow({ from: window.from, to: v }, minute);
          }}
        />
      </InputGroup>
    </div>
  );
}
