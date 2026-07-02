import type {
  CpuProfileNode,
  CpuProfilePayload,
} from "../shared/cpu-profile";

// CPU sampling profile capture for a hung renderer (MUL-3738, P0①).
//
// When the renderer hangs, the main process is still alive. It attaches the
// Chrome DevTools Protocol (CDP) debugger to the renderer's webContents and
// runs the V8 sampling profiler. The sampler runs on its own thread, so it
// records the JS call stack of the blocked main thread even while that thread
// is 100% stuck in synchronous JS — which is exactly the case we can't see any
// other way (the in-thread freeze watchdog only reports a duration, never the
// stack).
//
// PRIVACY — hard constraints (Kim v2 RFC + Howard APPROVED, MUL-3738):
//
//   * CDP ALLOWLIST: only the `Profiler.*` domain may ever be sent. Every CDP
//     call goes through `sendProfilerCommand`, which throws on any other
//     method. `HeapProfiler.*` (object/heap content), `Runtime.evaluate` /
//     `Runtime.getProperties` / `Runtime.callFunctionOn` (arbitrary reads), and
//     `Debugger.*` are forbidden. A source-level test pins the allowlist so a
//     future "just grab a bit of context" edit can't widen it past CI.
//
//   * CONTENT-FREE PAYLOAD: a CPU sampling profile is code-location data only
//     (function name, script URL, line/column, hit counts). We still copy out
//     only the whitelisted fields below, dropping anything the engine adds
//     (e.g. `deoptReason`, `positionTicks`). `url` is redacted by the renderer
//     before egress (shared `$exception` redaction); see App.tsx flush.
//
//   * DISCARD, NEVER TRUNCATE: an over-size profile is dropped whole. Truncating
//     a structured profile would yield invalid JSON and could strand a partial
//     frame.
//
//   * BEST-EFFORT: any failure (attach denied, renderer already gone, CDP
//     error) resolves to null and never throws into the recovery path.

/** The single CDP domain this module is permitted to drive. */
export const ALLOWED_CDP_METHOD_PREFIX = "Profiler.";

/** Minimal CDP debugger surface — matches Electron's `webContents.debugger`. */
export interface CdpDebugger {
  isAttached(): boolean;
  attach(protocolVersion?: string): void;
  detach(): void;
  sendCommand(method: string, commandParams?: Record<string, unknown>): Promise<unknown>;
}

export interface CaptureCpuProfileOptions {
  /** How long to sample the hung renderer. Bounded so the recovery path can't stall. */
  sampleDurationMs?: number;
  /**
   * Hard ceiling on the whole capture (real wall-clock timer). If CDP itself
   * hangs on a fully-stuck renderer, the capture resolves to null at this
   * bound so it can never wedge the reload prompt. Always longer than the
   * sample window.
   */
  hardTimeoutMs?: number;
  /** Serialized-size ceiling; a larger profile is discarded whole. */
  maxBytes?: number;
  /** Injected sleep for the sample window, overridable in tests. */
  delayMs?: (ms: number) => Promise<void>;
}

const DEFAULT_SAMPLE_DURATION_MS = 1200;
const DEFAULT_HARD_TIMEOUT_MS = 3000;
const DEFAULT_MAX_BYTES = 256 * 1024;

/**
 * Send a single CDP command, enforcing the `Profiler.*` allowlist. This is the
 * ONLY path to `debugger.sendCommand` in this module — nothing here may call
 * `sendCommand` directly. Throws (rejects) for any non-`Profiler.` method so a
 * forbidden command fails loudly in tests instead of silently exfiltrating.
 */
export function sendProfilerCommand(
  dbg: CdpDebugger,
  method: string,
  commandParams?: Record<string, unknown>,
): Promise<unknown> {
  if (!method.startsWith(ALLOWED_CDP_METHOD_PREFIX)) {
    return Promise.reject(
      new Error(
        `Forbidden CDP method "${method}": renderer profiling may only send ${ALLOWED_CDP_METHOD_PREFIX}* commands`,
      ),
    );
  }
  return dbg.sendCommand(method, commandParams);
}

/**
 * Attach, sample, and detach — returning a sanitized, content-free CPU profile
 * or null. Best-effort: returns null rather than throwing on any failure, and
 * always detaches if it attached.
 */
export async function captureRendererCpuProfile(
  dbg: CdpDebugger,
  options: CaptureCpuProfileOptions = {},
): Promise<CpuProfilePayload | null> {
  const hardTimeoutMs = options.hardTimeoutMs ?? DEFAULT_HARD_TIMEOUT_MS;

  // Real wall-clock guard so a hung CDP can never block the recovery prompt:
  // whichever finishes first wins, and the timer is cleared on completion.
  let timer: ReturnType<typeof setTimeout> | undefined;
  const guard = new Promise<null>((resolveGuard) => {
    timer = setTimeout(() => resolveGuard(null), hardTimeoutMs);
  });
  try {
    return await Promise.race([runCpuProfileCapture(dbg, options), guard]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

async function runCpuProfileCapture(
  dbg: CdpDebugger,
  options: CaptureCpuProfileOptions,
): Promise<CpuProfilePayload | null> {
  const sampleDurationMs = options.sampleDurationMs ?? DEFAULT_SAMPLE_DURATION_MS;
  const maxBytes = options.maxBytes ?? DEFAULT_MAX_BYTES;
  const sleep = options.delayMs ?? defaultDelay;

  let attachedHere = false;
  try {
    if (!dbg.isAttached()) {
      dbg.attach("1.3");
      attachedHere = true;
    }
    await sendProfilerCommand(dbg, "Profiler.enable");
    await sendProfilerCommand(dbg, "Profiler.start");
    await sleep(sampleDurationMs);
    const stopped = (await sendProfilerCommand(dbg, "Profiler.stop")) as
      | { profile?: unknown }
      | undefined;
    return sanitizeCpuProfile(stopped?.profile, maxBytes);
  } catch {
    // Best-effort: a hung/gone renderer or a denied attach must not break the
    // reload prompt. Drop the profile and move on.
    return null;
  } finally {
    try {
      await sendProfilerCommand(dbg, "Profiler.disable");
    } catch {
      // ignore — disable is best-effort cleanup
    }
    if (attachedHere) {
      try {
        dbg.detach();
      } catch {
        // ignore — detach is best-effort cleanup
      }
    }
  }
}

/**
 * Copy a raw V8 profile into the content-free whitelist shape, or return null
 * if the profile is malformed or serializes larger than `maxBytes` (discarded
 * whole — never truncated).
 */
export function sanitizeCpuProfile(
  rawProfile: unknown,
  maxBytes: number,
): CpuProfilePayload | null {
  if (!rawProfile || typeof rawProfile !== "object") return null;
  const raw = rawProfile as Record<string, unknown>;
  if (!Array.isArray(raw.nodes)) return null;

  const nodes: CpuProfileNode[] = [];
  for (const rawNode of raw.nodes) {
    if (!rawNode || typeof rawNode !== "object") continue;
    const node = rawNode as Record<string, unknown>;
    const frame = (node.callFrame ?? {}) as Record<string, unknown>;
    nodes.push({
      id: toNumber(node.id),
      callFrame: {
        functionName: toStringValue(frame.functionName),
        url: toStringValue(frame.url),
        lineNumber: toNumber(frame.lineNumber),
        columnNumber: toNumber(frame.columnNumber),
      },
      hitCount: toNumber(node.hitCount),
      ...(Array.isArray(node.children)
        ? { children: node.children.map(toNumber) }
        : {}),
    });
  }

  const payload: CpuProfilePayload = {
    nodes,
    startTime: toNumber(raw.startTime),
    endTime: toNumber(raw.endTime),
    ...(Array.isArray(raw.samples) ? { samples: raw.samples.map(toNumber) } : {}),
    ...(Array.isArray(raw.timeDeltas)
      ? { timeDeltas: raw.timeDeltas.map(toNumber) }
      : {}),
  };

  // Discard whole when over budget — never truncate a structured profile.
  if (JSON.stringify(payload).length > maxBytes) return null;
  return payload;
}

function toNumber(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function toStringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function defaultDelay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
