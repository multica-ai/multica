/**
 * The shape a hang stack is allowed to have, and the only code that builds it.
 *
 * Frames cross three boundaries between capture and telemetry: main writes
 * them into an on-disk breadcrumb, preload hands that file back over IPC, and
 * the renderer turns it into an event. The breadcrumb is barely validated on
 * read (it only has to survive version skew), so "sanitized once at capture"
 * is not a property the egress side can rely on — an older file, a corrupt
 * file, or a future main-process change could put anything in there.
 *
 * Both ends therefore rebuild frames through this module rather than trusting
 * the other. Whitelisting twice is cheap; a `scopeChain` handle reaching
 * PostHog is not.
 */

export interface HangStackFrame {
  functionName: string;
  url: string;
  lineNumber: number;
  columnNumber: number;
}

/** Deepest frames are the least useful; keep the top of the stack. */
export const HANG_STACK_MAX_FRAMES = 20;

/**
 * Rebuild frames from untrusted input, keeping only the four scalar fields.
 *
 * Everything else is dropped by construction — notably `scopeChain` and
 * `this`, whose object handles can be dereferenced into live user data, and
 * any key a future protocol version or a future writer might add. Returns null
 * when there is nothing usable, so callers can omit the field entirely rather
 * than ship an empty array.
 */
export function sanitizeHangStackFrames(
  rawFrames: unknown,
  maxFrames: number = HANG_STACK_MAX_FRAMES,
): HangStackFrame[] | null {
  if (!Array.isArray(rawFrames) || rawFrames.length === 0) return null;

  const frames: HangStackFrame[] = [];
  for (const raw of rawFrames.slice(0, maxFrames)) {
    if (!raw || typeof raw !== "object") continue;
    const frame = raw as Record<string, unknown>;
    // CDP nests the position under `location`; a breadcrumb frame has already
    // been flattened. Accept both so a re-sanitize is lossless.
    const location = (frame.location ?? frame) as Record<string, unknown>;
    frames.push({
      functionName:
        typeof frame.functionName === "string" && frame.functionName
          ? frame.functionName
          : "(anonymous)",
      url: redactFrameUrl(frame.url),
      lineNumber: toNumber(location.lineNumber),
      columnNumber: toNumber(location.columnNumber),
    });
  }
  return frames.length > 0 ? frames : null;
}

/**
 * Keep only the bundle-relative tail of a script URL. The full value is a
 * `file://` path into the install location and can carry the OS username.
 * Idempotent, so running it again on an already-reduced value is a no-op —
 * which is what lets the egress side re-apply it to a frame of unknown origin.
 */
export function redactFrameUrl(url: unknown): string {
  if (typeof url !== "string" || !url) return "";
  const [withoutQuery = ""] = url.split(/[?#]/);
  const segments = withoutQuery.split("/").filter(Boolean);
  if (segments.length === 0) return "";
  return segments.slice(-2).join("/");
}

function toNumber(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}
