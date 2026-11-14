// In-memory recording of what this client did recently: API requests, route
// changes, long tasks, errors, WebSocket state and events, and heap size. It
// backs the development toolbar's panels and is only installed by that
// toolbar, so production builds never record anything.

import type { QueryCacheNotifyEvent, QueryClient } from "@tanstack/react-query";
import { setApiRequestObserver } from "../api/request-observer";
import {
  activeSyncCauses,
  dropRealtimeConnection,
  replayRealtimeEvent,
  setRealtimeObserver,
} from "../api/realtime-observer";
import { getCurrentWsId } from "../platform/workspace-storage";
import type { WSMessage } from "../types/events";

export interface DevRequest {
  requestId: string;
  method: string;
  /** Sensitive query parameters are redacted. */
  path: string;
  /** JSON body with sensitive fields redacted; see `bodyNote` for what is missing. */
  body: string | null;
  /** Why `body` is not the whole request body, so it cannot be replayed as is. */
  bodyNote: string | null;
  /** Epoch ms. */
  startedAt: number;
  /** Null while in flight. */
  durationMs: number | null;
  /** Null while in flight, or when no response arrived. */
  status: number | null;
  /** Server error message, network error, or `aborted`. */
  error: string | null;
}

export interface DevNavigation {
  at: number;
  route: string;
  /** Input-to-paint time; null when no input explains the change. */
  durationMs: number | null;
}

export interface DevLongTask {
  at: number;
  durationMs: number;
  route: string | null;
}

export type DevErrorSource = "console" | "uncaught" | "rejection";

export interface DevError {
  at: number;
  source: DevErrorSource;
  message: string;
  stack: string | null;
  route: string | null;
}

export interface DevSyncChange {
  at: number;
  status: string;
}

export interface DevMemorySample {
  at: number;
  bytes: number;
}

export type DevCacheChange = "written" | "invalidated" | "removed" | "refetched";

/** Query hashes an event changed, by how. */
export type DevCacheEffects = Readonly<Record<DevCacheChange, readonly string[]>>;

export interface DevRealtimeEvent {
  id: number;
  at: number;
  /** A frame from the server, a recorded frame run again locally, or the socket coming back. */
  kind: "frame" | "replay" | "reconnect";
  /** Event type such as `issue:updated`; `reconnect` for a reconnect. */
  type: string;
  issueId: string | null;
  /** Issue key such as `MUL-12`, when the payload carries one. */
  issueKey: string | null;
  /** JSON payload with sensitive fields redacted; null for a reconnect. */
  payload: string | null;
  /** Why `payload` is not the whole payload. */
  payloadNote: string | null;
  /** `member <id>` or `agent <id>` when the frame names who caused it. */
  actor: string | null;
  /** Workspace that was current when the event arrived; replay runs only there. */
  workspaceId: string | null;
  /**
   * What handling the event did to the query cache, including refreshes it
   * deferred to a timer. Work a handler does after an `await` is not seen.
   */
  cache: DevCacheEffects;
  /** Reconnects: how long the socket was down, when the sync history shows it. */
  downMs: number | null;
}

/** Frames received during one second, starting at `at`. */
export interface DevFrameCount {
  at: number;
  count: number;
}

export interface DevRecording {
  requests: readonly DevRequest[];
  navigations: readonly DevNavigation[];
  longTasks: readonly DevLongTask[];
  errors: readonly DevError[];
  sync: readonly DevSyncChange[];
  events: readonly DevRealtimeEvent[];
  /** Server frames per second, kept apart from `events` so its cap never lowers the rate. */
  frameCounts: readonly DevFrameCount[];
  memory: readonly DevMemorySample[];
  /** JS heap ceiling on engines that report one. */
  heapLimit: number | null;
}

export const DEV_RECORDING_WINDOW_MS = 5 * 60_000;

const CAPS: Record<Exclude<keyof DevRecording, "heapLimit">, number> = {
  requests: 300,
  navigations: 50,
  longTasks: 200,
  errors: 100,
  sync: 50,
  events: 500,
  frameCounts: DEV_RECORDING_WINDOW_MS / 1000,
  memory: 150,
};

const BODY_MAX_CHARS = 2_000;
const PAYLOAD_MAX_CHARS = 4_000;
// A reconnect invalidates a few hundred queries; listing more says nothing new.
const CACHE_KEYS_MAX = 500;
const MESSAGE_MAX_CHARS = 1_000;
const MEMORY_SAMPLE_MS = 2_000;
const PRUNE_INTERVAL_MS = 5_000;
// Panels re-render on every change; coalescing keeps a burst of requests, or
// an error logged while rendering, from turning into a render per entry.
const NOTIFY_DELAY_MS = 100;

const EMPTY: DevRecording = {
  requests: [],
  navigations: [],
  longTasks: [],
  errors: [],
  sync: [],
  events: [],
  frameCounts: [],
  memory: [],
  heapLimit: null,
};

const NO_CACHE_EFFECTS: DevCacheEffects = { written: [], invalidated: [], removed: [], refetched: [] };

let recording: DevRecording = EMPTY;
let currentRoute: string | null = null;
const listeners = new Set<() => void>();
let notifyTimer: ReturnType<typeof setTimeout> | null = null;
let teardown: (() => void) | null = null;

// Per recorded event, keyed by id and dropped with the event.
let nextEventId = 1;
const eventMessages = new Map<number, WSMessage>();
const replayIds = new Set<number>();
// Cache changes arrive one query at a time, often hundreds per event. They
// collect here and reach `recording` when it is next read or published.
const cacheEffects = new Map<number, Record<DevCacheChange, Set<string>>>();
const dirtyEffects = new Set<number>();

function scheduleNotify() {
  if (notifyTimer) return;
  notifyTimer = setTimeout(() => {
    notifyTimer = null;
    applyCacheEffects();
    for (const listener of listeners) listener();
  }, NOTIFY_DELAY_MS);
}

function applyCacheEffects() {
  if (dirtyEffects.size === 0) return;
  recording = {
    ...recording,
    events: recording.events.map((event) => {
      const effects = dirtyEffects.has(event.id) ? cacheEffects.get(event.id) : undefined;
      if (!effects) return event;
      return {
        ...event,
        cache: {
          written: [...effects.written],
          invalidated: [...effects.invalidated],
          removed: [...effects.removed],
          refetched: [...effects.refetched],
        },
      };
    }),
  };
  dirtyEffects.clear();
}

/** Forget per-event state for events no longer in the recording. */
function forgetDroppedEvents() {
  const oldest = recording.events[0]?.id ?? nextEventId;
  for (const ids of [eventMessages, cacheEffects, replayIds, dirtyEffects]) {
    for (const id of ids.keys()) if (id < oldest) ids.delete(id);
  }
}

function prune<T extends { at: number }>(items: readonly T[], cap: number, now: number): T[] {
  const fresh = items.filter((item) => now - item.at <= DEV_RECORDING_WINDOW_MS);
  return fresh.length > cap ? fresh.slice(fresh.length - cap) : fresh;
}

/** Requests still in flight stay however old they are: they have not ended yet. */
function pruneRequests(requests: readonly DevRequest[], now: number): DevRequest[] {
  const kept = requests.filter((r) => r.durationMs === null || now - r.startedAt <= DEV_RECORDING_WINDOW_MS);
  return kept.length > CAPS.requests ? kept.slice(kept.length - CAPS.requests) : kept;
}

/** Drop what has aged out of the window, so idle time empties the panels too. */
function pruneExpired(now: number) {
  const next: DevRecording = {
    ...recording,
    requests: pruneRequests(recording.requests, now),
    navigations: prune(recording.navigations, CAPS.navigations, now),
    longTasks: prune(recording.longTasks, CAPS.longTasks, now),
    errors: prune(recording.errors, CAPS.errors, now),
    sync: prune(recording.sync, CAPS.sync, now),
    events: prune(recording.events, CAPS.events, now),
    frameCounts: prune(recording.frameCounts, CAPS.frameCounts, now),
    memory: prune(recording.memory, CAPS.memory, now),
  };
  const changed = (Object.keys(CAPS) as (keyof typeof CAPS)[]).some(
    (key) => next[key].length !== recording[key].length,
  );
  if (!changed) return;
  update(next);
  forgetDroppedEvents();
}

// Names of fields and query parameters that carry credentials: VCS and bot
// tokens, webhook secrets and URLs, passwords, agent environment variables.
// Numbers are never redacted, so usage counts such as `input_tokens` survive.
const SENSITIVE_KEY =
  /(token|secret|password|passphrase|credential|api_?key|private_?key|webhook_url|env)s?$/i;
const REDACTED = "[redacted]";

function redactValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redactValue);
  if (value === null || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value).map(([key, field]) => [
      key,
      SENSITIVE_KEY.test(key) && field !== null && typeof field !== "number" && typeof field !== "boolean"
        ? REDACTED
        : redactValue(field),
    ]),
  );
}

/** The path with sensitive query parameters redacted. */
export function redactPath(path: string): string {
  const query = path.indexOf("?");
  if (query < 0) return path;
  const params = new URLSearchParams(path.slice(query + 1));
  let redacted = false;
  for (const key of [...params.keys()]) {
    if (!SENSITIVE_KEY.test(key)) continue;
    params.set(key, REDACTED);
    redacted = true;
  }
  return redacted ? `${path.slice(0, query)}?${params.toString()}` : path;
}

/**
 * What of a request body can be kept for display and replay: JSON with
 * sensitive fields redacted, cut to a bounded size. Anything else is not kept,
 * since there is no telling what in it is a secret.
 */
export function recordableBody(body: string | null): Pick<DevRequest, "body" | "bodyNote"> {
  if (body === null) return { body: null, bodyNote: null };
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return { body: null, bodyNote: `not JSON (${body.length} characters), so not recorded` };
  }
  const redacted = JSON.stringify(redactValue(parsed));
  if (redacted.length <= BODY_MAX_CHARS) return { body: redacted, bodyNote: null };
  return {
    body: truncate(redacted, BODY_MAX_CHARS),
    bodyNote: `truncated to ${BODY_MAX_CHARS} of ${redacted.length} characters`,
  };
}

/** A WebSocket payload as kept for display: redacted like request bodies, cut to a bounded size. */
export function recordablePayload(payload: unknown): Pick<DevRealtimeEvent, "payload" | "payloadNote"> {
  let text: string;
  try {
    text = JSON.stringify(redactValue(payload)) ?? "null";
  } catch {
    return { payload: null, payloadNote: "not serializable, so not recorded" };
  }
  if (text.length <= PAYLOAD_MAX_CHARS) return { payload: text, payloadNote: null };
  return {
    payload: truncate(text, PAYLOAD_MAX_CHARS),
    payloadNote: `truncated to ${PAYLOAD_MAX_CHARS} of ${text.length} characters`,
  };
}

function nonEmptyString(value: unknown): string | null {
  return typeof value === "string" && value ? value : null;
}

/** The issue an event is about, from the payload shapes that name one. */
export function eventIssue(payload: unknown): Pick<DevRealtimeEvent, "issueId" | "issueKey"> {
  if (!payload || typeof payload !== "object") return { issueId: null, issueKey: null };
  const fields = payload as Record<string, unknown>;
  const nested = (key: string) =>
    fields[key] && typeof fields[key] === "object" ? (fields[key] as Record<string, unknown>) : null;
  const issue = nested("issue");
  return {
    issueId:
      nonEmptyString(issue?.id) ??
      nonEmptyString(fields.issue_id) ??
      nonEmptyString(nested("comment")?.issue_id) ??
      nonEmptyString(nested("item")?.issue_id),
    issueKey: nonEmptyString(issue?.identifier),
  };
}

function recordEvent(event: Omit<DevRealtimeEvent, "id" | "cache">): number {
  const id = nextEventId++;
  append("events", { ...event, id, cache: NO_CACHE_EFFECTS });
  forgetDroppedEvents();
  return id;
}

function countFrame(now: number) {
  const second = now - (now % 1000);
  const counts = recording.frameCounts;
  const last = counts[counts.length - 1];
  const next =
    last?.at === second
      ? [...counts.slice(0, -1), { at: second, count: last.count + 1 }]
      : prune([...counts, { at: second, count: 1 }], CAPS.frameCounts, now);
  update({ frameCounts: next });
}

function recordFrame(msg: WSMessage, replayed: boolean): number {
  const now = Date.now();
  if (!replayed) countFrame(now);
  const id = recordEvent({
    at: now,
    kind: replayed ? "replay" : "frame",
    type: msg.type,
    ...eventIssue(msg.payload),
    ...recordablePayload(msg.payload),
    actor: msg.actor_type && msg.actor_id ? `${msg.actor_type} ${msg.actor_id}` : null,
    workspaceId: getCurrentWsId(),
    downMs: null,
  });
  eventMessages.set(id, msg);
  if (replayed) replayIds.add(id);
  return id;
}

/** How long the socket has been down, from the last unbroken run of non-connected states. */
function downtime(now: number): number | null {
  const { sync } = recording;
  let end = sync.length - 1;
  let upAt = now;
  // The status hook may already have recorded the connection coming back.
  while (end >= 0 && sync[end]!.status === "connected") upAt = sync[end--]!.at;
  if (end < 0) return null;
  let start = end;
  while (start > 0 && sync[start - 1]!.status !== "connected") start -= 1;
  return upAt - sync[start]!.at;
}

function recordReconnect(): number {
  const now = Date.now();
  return recordEvent({
    at: now,
    kind: "reconnect",
    type: "reconnect",
    issueId: null,
    issueKey: null,
    payload: null,
    payloadNote: null,
    actor: null,
    workspaceId: getCurrentWsId(),
    downMs: downtime(now),
  });
}

function cacheChange(event: QueryCacheNotifyEvent): DevCacheChange | null {
  if (event.type === "removed") return "removed";
  if (event.type !== "updated") return null;
  const { action } = event;
  if (action.type === "success" && action.manual) return "written";
  if (action.type === "invalidate") return "invalidated";
  if (action.type === "fetch") return "refetched";
  return null;
}

/** Credit a cache change to the realtime events whose handling is running. */
function onCacheEvent(event: QueryCacheNotifyEvent) {
  const causes = activeSyncCauses();
  if (causes.length === 0) return;
  const change = cacheChange(event);
  if (!change) return;
  for (const id of causes) {
    let effects = cacheEffects.get(id);
    if (!effects) {
      effects = { written: new Set(), invalidated: new Set(), removed: new Set(), refetched: new Set() };
      cacheEffects.set(id, effects);
    }
    if (effects[change].size >= CACHE_KEYS_MAX) continue;
    effects[change].add(event.query.queryHash);
    dirtyEffects.add(id);
  }
  scheduleNotify();
}

function update(patch: Partial<DevRecording>) {
  recording = { ...recording, ...patch };
  scheduleNotify();
}

function append<K extends Exclude<keyof DevRecording, "heapLimit" | "requests">>(
  key: K,
  item: DevRecording[K][number],
) {
  const list = [...(recording[key] as readonly { at: number }[]), item as { at: number }];
  update({ [key]: prune(list, CAPS[key], Date.now()) } as Partial<DevRecording>);
}

function truncate(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max)}…` : text;
}

/** `console.error("%s failed: %o", a, b)` as the console would print it. */
export function formatConsoleArgs(args: readonly unknown[]): { message: string; stack: string | null } {
  const rest = [...args];
  let stack: string | null = null;
  const show = (value: unknown): string => {
    if (value instanceof Error) {
      stack ??= value.stack ?? null;
      return `${value.name}: ${value.message}`;
    }
    if (typeof value === "string") return value;
    try {
      return JSON.stringify(value) ?? String(value);
    } catch {
      return String(value);
    }
  };
  let message = "";
  if (typeof rest[0] === "string") {
    message = (rest.shift() as string).replace(/%[sdifoOc]/g, (token) =>
      token === "%c" ? (rest.shift(), "") : rest.length ? show(rest.shift()) : token,
    );
  }
  const tail = rest.map(show).join(" ");
  message = [message, tail].filter(Boolean).join(" ");
  return { message: truncate(message, MESSAGE_MAX_CHARS), stack };
}

/**
 * The API client logs every failed response (`createLogger("api")`, message
 * `← 500 /path`). Requests already carry that failure, with more detail, so
 * counting the log line too would report each failure twice.
 */
function isApiResponseLog(args: readonly unknown[]): boolean {
  return (
    typeof args[0] === "string" &&
    args[0].endsWith("[api]") &&
    typeof args[2] === "string" &&
    args[2].startsWith("← ")
  );
}

function recordError(source: DevErrorSource, message: string, stack: string | null) {
  append("errors", { at: Date.now(), source, message, stack, route: currentRoute });
}

function readHeap(): { used: number; limit: number } | null {
  const memory = (
    performance as Performance & { memory?: { usedJSHeapSize: number; jsHeapSizeLimit: number } }
  ).memory;
  return memory ? { used: memory.usedJSHeapSize, limit: memory.jsHeapSizeLimit } : null;
}

/** A write sent while a replayed event is being handled; replays stay local. */
function refuseReplayWrite({ method, readOnly }: { method: string; readOnly: boolean }): string | null {
  if (readOnly || method === "GET" || method === "HEAD") return null;
  const replaying = activeSyncCauses().some((id) => replayIds.has(id));
  return replaying ? `${method} not sent: a replayed realtime event runs locally only` : null;
}

/**
 * Start recording. With a query client, realtime events are tagged with the
 * cache changes they caused. Returns a function that removes every hook the
 * recorder installed and drops what it recorded, so the next start is fresh.
 * A no-op outside the browser, or while a recording is already running (the
 * returned function then leaves that recording alone).
 */
export function installDevRecorder(queryClient?: QueryClient): () => void {
  if (teardown || typeof window === "undefined") return () => {};
  const cleanups: (() => void)[] = [];

  setRealtimeObserver({ onEvent: recordFrame, onReconnect: recordReconnect });
  cleanups.push(() => setRealtimeObserver(null));
  if (queryClient) cleanups.push(queryClient.getQueryCache().subscribe(onCacheEvent));

  setApiRequestObserver({
    refuse: refuseReplayWrite,
    onStart: ({ requestId, method, path, body }) => {
      const request: DevRequest = {
        requestId,
        method,
        path: redactPath(path),
        ...recordableBody(body),
        startedAt: Date.now(),
        durationMs: null,
        status: null,
        error: null,
      };
      update({ requests: pruneRequests([...recording.requests, request], request.startedAt) });
    },
    onEnd: ({ requestId, status, error }) => {
      const now = Date.now();
      update({
        requests: recording.requests.map((r) =>
          r.requestId === requestId
            ? { ...r, status, error: error === null ? null : truncate(error, MESSAGE_MAX_CHARS), durationMs: now - r.startedAt }
            : r,
        ),
      });
    },
  });
  cleanups.push(() => setApiRequestObserver(null));

  if (PerformanceObserver.supportedEntryTypes?.includes("longtask")) {
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        append("longTasks", {
          at: performance.timeOrigin + entry.startTime,
          durationMs: entry.duration,
          route: currentRoute,
        });
      }
    });
    observer.observe({ type: "longtask" });
    cleanups.push(() => observer.disconnect());
  }

  const onError = (event: ErrorEvent) => {
    // Chromium reports a skipped resize callback as an error; it is not a bug.
    if (event.message.startsWith("ResizeObserver loop")) return;
    const error = event.error instanceof Error ? event.error : null;
    recordError("uncaught", error ? `${error.name}: ${error.message}` : event.message, error?.stack ?? null);
  };
  const onRejection = (event: PromiseRejectionEvent) => {
    const { message, stack } = formatConsoleArgs([event.reason]);
    recordError("rejection", message, stack);
  };
  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onRejection);
  cleanups.push(() => {
    window.removeEventListener("error", onError);
    window.removeEventListener("unhandledrejection", onRejection);
  });

  const originalConsoleError = console.error;
  console.error = (...args: unknown[]) => {
    originalConsoleError.apply(console, args);
    if (isApiResponseLog(args)) return;
    const { message, stack } = formatConsoleArgs(args);
    recordError("console", message, stack);
  };
  cleanups.push(() => {
    console.error = originalConsoleError;
  });

  const sampleHeap = () => {
    const heap = readHeap();
    if (!heap) return;
    recording = { ...recording, heapLimit: heap.limit };
    append("memory", { at: Date.now(), bytes: heap.used });
  };
  sampleHeap();
  const heapTimer = setInterval(sampleHeap, MEMORY_SAMPLE_MS);
  const pruneTimer = setInterval(() => pruneExpired(Date.now()), PRUNE_INTERVAL_MS);
  cleanups.push(() => {
    clearInterval(heapTimer);
    clearInterval(pruneTimer);
  });

  const stop = () => cleanups.forEach((cleanup) => cleanup());
  teardown = stop;
  return () => {
    if (teardown === stop) stopDevRecorder();
  };
}

function stopDevRecorder(): void {
  teardown?.();
  teardown = null;
  if (notifyTimer) clearTimeout(notifyTimer);
  notifyTimer = null;
  recording = EMPTY;
  currentRoute = null;
  nextEventId = 1;
  eventMessages.clear();
  replayIds.clear();
  cacheEffects.clear();
  dirtyEffects.clear();
}

/** The current route, so later entries say where they happened. */
export function recordDevRoute(route: string): void {
  if (route === currentRoute) return;
  currentRoute = route;
  append("navigations", { at: Date.now(), route, durationMs: null });
}

/** Attach the measured input-to-paint time to the latest visit of `route`. */
export function recordDevNavigationDuration(route: string, durationMs: number): void {
  const navigations = [...recording.navigations];
  for (let i = navigations.length - 1; i >= 0; i -= 1) {
    if (navigations[i]!.route !== route) continue;
    navigations[i] = { ...navigations[i]!, durationMs };
    update({ navigations });
    return;
  }
}

export function recordDevSyncStatus(status: string): void {
  if (recording.sync.at(-1)?.status === status) return;
  append("sync", { at: Date.now(), status });
}

export function clearDevRequests(): void {
  update({ requests: recording.requests.filter((r) => r.durationMs === null) });
}

export function clearDevErrors(): void {
  update({ errors: [] });
}

export function clearDevEvents(): void {
  update({ events: [], frameCounts: [] });
  forgetDroppedEvents();
}

/**
 * Run a recorded event through realtime sync's handlers again, recorded as a
 * new `replay` event. Only cache handling runs: no other subscriber sees it,
 * and a write request made while handling it is refused. Handlers write into
 * the current workspace's caches, so an event recorded in another workspace
 * does not run. Returns why it did not run, or null when it did.
 */
export function replayDevEvent(id: number): string | null {
  const msg = eventMessages.get(id);
  const event = recording.events.find((e) => e.id === id);
  if (!msg || !event) return "the event aged out of the recording";
  if (event.workspaceId !== getCurrentWsId()) return "it was recorded in another workspace";
  return replayRealtimeEvent(msg) ? null : "realtime sync is not running";
}

/** Close the live socket as a network drop would, to watch the reconnect. */
export function dropDevConnection(): boolean {
  return dropRealtimeConnection();
}

export function subscribeDevRecording(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getDevRecording(): DevRecording {
  applyCacheEffects();
  return recording;
}

/** Test seam: stop recording and drop everything recorded. */
export function resetDevRecorder(): void {
  stopDevRecorder();
  listeners.clear();
}
