import {
  DEV_RECORDING_WINDOW_MS,
  type DevLongTask,
  type DevLongTaskScript,
  type DevReactUpdate,
  type DevRecording,
} from "@multica/core/diagnostics";
import { eventSubject, formatClock, formatDuration } from "./metrics";

/** Activity just before a task is what most often set it off: a response, an event, a route change. */
const LEAD_MS = 1_000;
const NAV_LEAD_MS = 2_000;
const MAX_ACTIVITY = 8;
const MAX_DETAILED_TASKS = 20;
/** A component below this share of the task does not explain it. */
const CAUSE_MIN_SHARE = 0.2;
const MAX_TOTALS = 10;
const ERROR_MAX_CHARS = 200;

export interface TaskActivity {
  at: number;
  text: string;
}

/** `HH:MM:SS.mmm`, fine enough to order what happened inside one second. */
export function formatClockMs(at: number): string {
  return `${formatClock(at)}.${String(Math.floor(at % 1000)).padStart(3, "0")}`;
}

/** A script's source without origin or query, so a bundle path reads the same on any port. */
export function scriptSource(source: string): string {
  const match = /^(.*?)(:\d+)?$/.exec(source)!;
  const [, url = source, position = ""] = match;
  try {
    const parsed = new URL(url);
    const path = parsed.protocol === "file:" ? parsed.pathname.split("/").slice(-2).join("/") : parsed.pathname;
    return `${path}${position}`;
  } catch {
    return source;
  }
}

export function scriptLabel(script: DevLongTaskScript): string {
  const fn = script.functionName && !script.invoker.includes(script.functionName) ? ` → ${script.functionName}` : "";
  return `${script.invoker}${fn}`;
}

export function updateLabel(update: DevReactUpdate): string {
  return [
    update.kind,
    update.lane && `(${update.lane})`,
    update.component && `from ${update.component}`,
    update.method && `via ${update.method}`,
  ]
    .filter(Boolean)
    .join(" ");
}

/**
 * One line on what most likely made the task long, for the task list: the
 * component that took a real share of it, else the longest script.
 */
export function longTaskCause(task: DevLongTask): string | null {
  const attribution = task.attribution;
  if (!attribution) return null;
  const component = attribution.components[0];
  const script = attribution.scripts[0];
  if (component && (component.selfMs >= task.durationMs * CAUSE_MIN_SHARE || (!script && component.selfMs >= 1))) {
    return `<${component.name}> ${formatDuration(component.selfMs)}`;
  }
  return script ? scriptLabel(script) : null;
}

/** What else the recording saw around the task, oldest first. */
export function taskActivity(task: DevLongTask, recording: DevRecording): TaskActivity[] {
  const end = task.at + task.durationMs;
  const within = (at: number, lead: number, tail = 0) => at >= task.at - lead && at <= end + tail;
  const items: TaskActivity[] = [];
  for (const request of recording.requests) {
    const doneAt = request.durationMs === null ? null : request.startedAt + request.durationMs;
    const settled = doneAt !== null && within(doneAt, LEAD_MS);
    if (!settled && !within(request.startedAt, 0)) continue;
    const outcome =
      request.durationMs === null
        ? "in flight"
        : `${request.status ?? request.error ?? "no response"} in ${formatDuration(request.durationMs)}`;
    items.push({
      at: settled ? doneAt : request.startedAt,
      text: `${settled ? "response" : "request"} ${request.method} ${request.path} → ${outcome}`,
    });
  }
  for (const event of recording.events) {
    if (!within(event.at, LEAD_MS)) continue;
    const subject = eventSubject(event);
    items.push({ at: event.at, text: `realtime ${event.kind === "frame" ? "" : `${event.kind} `}${event.type}${subject ? ` ${subject}` : ""}` });
  }
  for (const navigation of recording.navigations) {
    if (within(navigation.at, NAV_LEAD_MS)) items.push({ at: navigation.at, text: `route → ${navigation.route}` });
  }
  for (const error of recording.errors) {
    if (!within(error.at, 0, 500)) continue;
    const message = error.message.length > ERROR_MAX_CHARS ? `${error.message.slice(0, ERROR_MAX_CHARS)}…` : error.message;
    items.push({ at: error.at, text: `${error.source} error: ${message}` });
  }
  // The earliest entries are the lead-up, the likeliest causes.
  return items.sort((a, b) => a.at - b.at).slice(0, MAX_ACTIVITY);
}

/** One task as Markdown: what ran, what React rendered, and what happened around it. */
export function longTaskDetail(task: DevLongTask, recording: DevRecording): string {
  const lines = [`### ${formatClockMs(task.at)} · ${formatDuration(task.durationMs)} · ${task.route ?? "unknown route"}`];
  const attribution = task.attribution;
  if (!attribution) {
    lines.push("", "Not attributed yet.");
  } else {
    const { frame, scripts, updates, components } = attribution;
    if (frame) {
      const render = frame.renderMs > 0 ? `; rendering ${formatDuration(frame.renderMs)} (style and layout ${formatDuration(frame.styleLayoutMs)})` : "";
      lines.push("", `Frame: ${formatDuration(frame.durationMs)}, ${formatDuration(frame.blockingMs)} blocking${render}`);
    }
    if (scripts.length > 0) {
      lines.push("", "Scripts, longest first:");
      for (const script of scripts) {
        const where = script.source ? ` at ${scriptSource(script.source)}` : "";
        const layout = script.forcedLayoutMs >= 1 ? `, forced layout ${formatDuration(script.forcedLayoutMs)}` : "";
        lines.push(`- ${formatDuration(script.durationMs)} ${scriptLabel(script)} [${script.invokerType}]${where}${layout}`);
      }
    }
    if (updates.length > 0) {
      lines.push("", "React updates rendered in this task:");
      for (const update of updates) lines.push(`- ${updateLabel(update)}`);
    }
    if (components.length > 0) {
      lines.push("", "React components with changed props, most self time first:");
      for (const component of components) {
        const props = component.changedProps.length > 0 ? `; changed props: ${component.changedProps.join(", ")}` : "";
        const deep = component.deeplyEqualProps ? "; props were deeply equal (new object, same contents)" : "";
        lines.push(`- <${component.name}> ${formatDuration(component.selfMs)} self, ${component.renders}×${props}${deep}`);
      }
    }
    if (!frame && scripts.length === 0 && updates.length === 0 && components.length === 0) {
      lines.push("", "Nothing attributed: no long animation frame or React measure covered this task.");
    }
  }
  const activity = taskActivity(task, recording);
  if (activity.length > 0) {
    lines.push("", "Around it:");
    for (const item of activity) lines.push(`- ${formatClockMs(item.at)} ${item.text}`);
  }
  return lines.join("\n");
}

function topTotals<T>(items: T[], key: (item: T) => string, ms: (item: T) => number) {
  const totals = new Map<string, { label: string; ms: number; count: number }>();
  for (const item of items) {
    const label = key(item);
    const total = totals.get(label) ?? { label, ms: 0, count: 0 };
    total.ms += ms(item);
    total.count += 1;
    totals.set(label, total);
  }
  return [...totals.values()].sort((a, b) => b.ms - a.ms || b.count - a.count).slice(0, MAX_TOTALS);
}

function timezone(at: number): string {
  const offset = -new Date(at).getTimezoneOffset();
  const sign = offset >= 0 ? "+" : "-";
  const abs = Math.abs(offset);
  return `UTC${sign}${String(Math.floor(abs / 60)).padStart(2, "0")}:${String(abs % 60).padStart(2, "0")}`;
}

export interface LongTaskReportContext {
  build: string;
  apiUrl: string;
  route: string;
  now: number;
  /** Share of the last 10s spent in long tasks; null where long tasks are not reported. */
  blocked: number | null;
  /** Whether the engine reports long animation frames, the source of script attribution. */
  framesSupported: boolean;
  userAgent: string;
}

/** The recorded long tasks as a Markdown report someone, or a model, can read without the app. */
export function longTaskReport(recording: DevRecording, context: LongTaskReportContext): string {
  const tasks = recording.longTasks;
  const total = tasks.reduce((sum, task) => sum + task.durationMs, 0);
  const withReact = tasks.filter((t) => t.attribution && (t.attribution.components.length > 0 || t.attribution.updates.length > 0)).length;
  const lines = [
    `## Long tasks · ${context.build} · ${context.apiUrl}`,
    "",
    `- Copied ${new Date(context.now).toISOString()}, covering the last ${DEV_RECORDING_WINDOW_MS / 60_000} min. Times below are local (${timezone(context.now)}).`,
    `- Current route: ${context.route}`,
    `- User agent: ${context.userAgent}`,
    `- ${tasks.length} long tasks, ${formatDuration(total)} blocked in total; blocked in the last 10s: ${context.blocked === null ? "not reported" : `${Math.round(context.blocked * 100)}%`}`,
    `- Script attribution: ${context.framesSupported ? "Long Animation Frames API" : "not reported by this engine"}. React measures in ${withReact} of ${tasks.length} tasks (development React builds only).`,
    "",
    "A long task kept the main thread busy for 50ms or more. Scripts are the entry points the browser ran during the task's frame. React components are renders whose props changed, the only ones React's development build measures; self time excludes measured child components. \"Around it\" lists what the client saw from 1s before the task (2s for route changes) until it ended.",
  ];
  if (tasks.length === 0) return [...lines, "", "No long tasks were recorded."].join("\n");

  const attributed = tasks.flatMap((t) => (t.attribution ? [t.attribution] : []));
  const components = topTotals(
    attributed.flatMap((a) => a.components),
    (c) => `<${c.name}>`,
    (c) => c.selfMs,
  );
  const scripts = topTotals(
    attributed.flatMap((a) => a.scripts),
    (s) => `${scriptLabel(s)}${s.source ? ` at ${scriptSource(s.source)}` : ""}`,
    (s) => s.durationMs,
  );
  const updates = topTotals(attributed.flatMap((a) => a.updates), updateLabel, () => 0);
  if (components.length + scripts.length + updates.length > 0) {
    lines.push("", "### Where the time went, across all tasks");
    if (components.length > 0) {
      lines.push("", "Components by self time:");
      for (const c of components) lines.push(`- ${c.label} ${formatDuration(c.ms)} over ${c.count} ${c.count === 1 ? "task" : "tasks"}`);
    }
    if (scripts.length > 0) {
      lines.push("", "Scripts by time:");
      for (const s of scripts) lines.push(`- ${s.label} ${formatDuration(s.ms)}, ${s.count}×`);
    }
    if (updates.length > 0) {
      lines.push("", "React updates:");
      for (const u of updates) lines.push(`- ${u.label}, ${u.count}×`);
    }
  }

  lines.push("", "### Every long task, oldest first", "", "```");
  for (const task of tasks) {
    lines.push(`${formatClockMs(task.at)}  ${formatDuration(task.durationMs).padStart(6)}  ${longTaskCause(task) ?? task.route ?? ""}`);
  }
  lines.push("```");

  const longest = [...tasks].sort((a, b) => b.durationMs - a.durationMs).slice(0, MAX_DETAILED_TASKS);
  lines.push("", `## The ${longest.length} longest tasks in detail`);
  for (const task of longest) lines.push("", longTaskDetail(task, recording));
  return lines.join("\n");
}
