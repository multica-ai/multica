// Session-scoped dedupe / throttle for `$exception` events.
//
// Runs in posthog-js `before_send` AFTER `redactExceptionProperties`, so the
// fingerprint is built purely from already-redacted fields — no raw message,
// value, or PII is ever written to storage (only a hash + a small counter).
//
// The fuse: keep the first EXCEPTION_SAMPLE_LIMIT of each (tab-session,
// fingerprint) pair and drop the rest. One runaway error — a render loop, a
// polling fetch that keeps throwing — otherwise emits 100+ identical
// `$exception` events per session (MUL-3331 / MUL-3330). Different fingerprints
// never affect each other.
//
// Safety invariant (load-bearing): `before_send` must never throw — a throw
// there breaks ALL event delivery — and every storage failure must fail OPEN.
// When in doubt we KEEP the event: emitting a duplicate is cheap, silently
// dropping a real first-occurrence error is not. setItem failures therefore
// only ever under-count (fewer drops), never over-drop.
//
// Scope is the browser tab session (`sessionStorage`): cleared when the tab
// closes, isolated per tab. This is intentionally NOT the posthog 30-min
// session — see the dedupe discussion on MUL-3331.

const STORAGE_KEY = "mc_exc_fp";
// Keep the first N of each fingerprint per session, drop from N+1.
const EXCEPTION_SAMPLE_LIMIT = 3;
// Cap distinct fingerprints tracked per session so a session that throws many
// *different* errors can't grow the blob without bound. Past the cap, new
// fingerprints are not tracked and fail open (kept).
const MAX_FINGERPRINTS = 50;

type FingerprintCounts = Record<string, number>;

/**
 * Decide whether this already-redacted `$exception` event should be dropped as
 * a session-level duplicate. Returns `true` to drop, `false` to keep.
 *
 * Never throws. Any missing fingerprint signal, unavailable/corrupt storage, or
 * unexpected error results in `false` (keep) — the fail-open direction.
 */
export function shouldDropException(
  properties: Record<string, unknown> | undefined,
): boolean {
  const fingerprint = buildFingerprint(properties);
  // Nothing stable to dedupe on → keep.
  if (fingerprint === null) return false;

  const storage = getSessionStorage();
  if (!storage) return false;

  // The entire read-decide-write sequence is guarded: a throw anywhere (parse,
  // getItem, property access) degrades to keep.
  try {
    const counts = readCounts(storage);
    const current = typeof counts[fingerprint] === "number" ? counts[fingerprint] : 0;

    // Already at the limit for this fingerprint → fuse blows, drop.
    if (current >= EXCEPTION_SAMPLE_LIMIT) return true;

    // A brand-new fingerprint once the cap is reached: don't track it (would
    // grow the blob), and keep the event.
    if (current === 0 && Object.keys(counts).length >= MAX_FINGERPRINTS) {
      return false;
    }

    counts[fingerprint] = current + 1;
    try {
      storage.setItem(STORAGE_KEY, JSON.stringify(counts));
    } catch {
      // Persisting the increment failed (quota / disabled). We still keep this
      // event (return false below). The unpersisted increment only means the
      // next identical error is also kept — under-counting toward the limit,
      // i.e. fewer drops, never more. This is the required failure direction.
    }
    return false;
  } catch {
    return false;
  }
}

/** Read and validate the counts blob. A corrupt or unexpected payload is
 *  treated as empty (fail open — this event is kept and re-seeds the blob). */
function readCounts(storage: Storage): FingerprintCounts {
  const raw = storage.getItem(STORAGE_KEY);
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as FingerprintCounts;
    }
  } catch {
    // Corrupt JSON blob → start fresh.
  }
  return {};
}

/**
 * Build a stable fingerprint from the redacted exception properties. Uses the
 * exception type, the redacted message/value, and a single deterministic stack
 * frame. Returns `null` when there's nothing stable to key on (keep the event).
 *
 * Every frame field (`function` / `lineno` / `colno`) is treated as optional
 * and degrades to empty — minified or partial stacks must not throw or collapse
 * every error into one bucket via an undefined access.
 */
function buildFingerprint(properties: Record<string, unknown> | undefined): string | null {
  if (!properties || typeof properties !== "object") return null;

  const list = properties.$exception_list;
  const entry =
    Array.isArray(list) && list.length > 0 && list[0] && typeof list[0] === "object"
      ? (list[0] as Record<string, unknown>)
      : undefined;

  const type = readString(entry?.type) ?? readString(properties.$exception_type) ?? "";
  const value =
    readString(entry?.value) ?? readString(properties.$exception_message) ?? "";
  const frame = topFrame(entry);

  // No signal at all → don't dedupe.
  if (type === "" && value === "" && !frame) return null;

  const parts = [type, value];
  if (frame) {
    // colno is kept (load-bearing): minified bundles collapse many statements
    // onto one line, so line alone under-discriminates distinct errors.
    parts.push(frame.filename, frame.fn, frame.lineno, frame.colno);
  }
  return hash(parts.join(""));
}

interface TopFrame {
  filename: string;
  fn: string;
  lineno: string;
  colno: string;
}

/**
 * Extract a single deterministic stack frame for fingerprinting. We always take
 * the LAST frame in the array — a fixed end, with NO engine/order detection.
 * The same error within a session yields the same frames array and therefore
 * the same chosen frame, which is all the fingerprint needs; we don't care
 * which end is semantically "topmost". Missing pieces degrade to "".
 */
function topFrame(entry: Record<string, unknown> | undefined): TopFrame | null {
  if (!entry) return null;
  const stacktrace = entry.stacktrace;
  const frames =
    stacktrace && typeof stacktrace === "object"
      ? (stacktrace as Record<string, unknown>).frames
      : undefined;
  if (!Array.isArray(frames) || frames.length === 0) return null;

  const f = frames[frames.length - 1];
  if (!f || typeof f !== "object") return null;
  const frame = f as Record<string, unknown>;

  return {
    filename: readString(frame.filename) ?? "",
    fn: readString(frame.function) ?? "",
    lineno: readNumberAsString(frame.lineno) ?? "",
    colno: readNumberAsString(frame.colno) ?? "",
  };
}

function readString(v: unknown): string | undefined {
  return typeof v === "string" && v.length > 0 ? v : undefined;
}

function readNumberAsString(v: unknown): string | undefined {
  return typeof v === "number" && Number.isFinite(v) ? String(v) : undefined;
}

/** djb2 — a tiny stable string hash. Only used to bound the storage-key length;
 *  collision risk across a single tab session's exceptions is negligible. */
function hash(input: string): string {
  let h = 5381;
  for (let i = 0; i < input.length; i++) {
    h = ((h << 5) + h) ^ input.charCodeAt(i);
  }
  return (h >>> 0).toString(36);
}

/** Resolve `sessionStorage`, returning `null` if it is absent (SSR) or throws
 *  on access (sandboxed iframe, storage disabled). */
function getSessionStorage(): Storage | null {
  try {
    if (typeof sessionStorage === "undefined") return null;
    return sessionStorage;
  } catch {
    return null;
  }
}
