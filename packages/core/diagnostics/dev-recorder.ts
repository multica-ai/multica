// In-memory recording of what this client did recently: API requests, route
// changes, long tasks, errors, WebSocket state and heap size. It backs the
// development toolbar's panels and is only installed by that toolbar, so
// production builds never record anything.

import { setApiRequestObserver } from "../api/request-observer";

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

export interface DevRecording {
  requests: readonly DevRequest[];
  navigations: readonly DevNavigation[];
  longTasks: readonly DevLongTask[];
  errors: readonly DevError[];
  sync: readonly DevSyncChange[];
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
  memory: 150,
};

const BODY_MAX_CHARS = 2_000;
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
  memory: [],
  heapLimit: null,
};

let recording: DevRecording = EMPTY;
let currentRoute: string | null = null;
const listeners = new Set<() => void>();
let notifyTimer: ReturnType<typeof setTimeout> | null = null;
let teardown: (() => void) | null = null;

function scheduleNotify() {
  if (notifyTimer) return;
  notifyTimer = setTimeout(() => {
    notifyTimer = null;
    for (const listener of listeners) listener();
  }, NOTIFY_DELAY_MS);
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
    memory: prune(recording.memory, CAPS.memory, now),
  };
  const changed = (Object.keys(CAPS) as (keyof typeof CAPS)[]).some(
    (key) => next[key].length !== recording[key].length,
  );
  if (changed) update(next);
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

/** Start recording. Idempotent, and a no-op outside the browser. */
export function installDevRecorder(): void {
  if (teardown || typeof window === "undefined") return;
  const cleanups: (() => void)[] = [];

  setApiRequestObserver({
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

  teardown = () => cleanups.forEach((cleanup) => cleanup());
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

export function subscribeDevRecording(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getDevRecording(): DevRecording {
  return recording;
}

/** Test seam: stop recording and drop everything recorded. */
export function resetDevRecorder(): void {
  teardown?.();
  teardown = null;
  if (notifyTimer) clearTimeout(notifyTimer);
  notifyTimer = null;
  recording = EMPTY;
  currentRoute = null;
  listeners.clear();
}
