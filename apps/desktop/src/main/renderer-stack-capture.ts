// JS call stack capture for a hung renderer (MUL-5345).
//
// When the renderer stops responding, the main process is still alive but has
// no way to ask the renderer anything — the thread that would answer is the
// thread that is stuck. The one channel that still gets through is the Chrome
// DevTools Protocol, and only if it was already open: attaching after the hang
// starts does not get dispatched. So the channel is warmed at window creation
// and `Debugger.pause` is sent when the hang is detected.
//
// Measured on Electron 39.8.7 / Chromium 142 (spike, MUL-5345):
//   * pause during a 12s synchronous block returned the stack in 2ms, top
//     frame correctly identified as the blocking function;
//   * keeping the channel warm for the whole session showed no benchmark cost
//     beyond run-to-run noise;
//   * DevTools and this channel coexist in both open orders — opening DevTools
//     does not evict us, and pause keeps working afterwards.
//
// PRIVACY — hard constraints:
//
//   * CDP ALLOWLIST: only the four `Debugger` verbs below may ever be sent.
//     Every command goes through `sendDebuggerCommand`, which throws on
//     anything else. `Runtime.evaluate` / `Runtime.getProperties` /
//     `Runtime.callFunctionOn` (arbitrary reads), `HeapProfiler.*` (heap
//     contents), and `Debugger.setBreakpoint*` are forbidden.
//   * CODE LOCATIONS ONLY: a paused frame also carries `scopeChain` handles
//     that can be dereferenced into live user data. We copy four scalar fields
//     per frame and drop everything else, so no such handle is ever retained.
//   * URLs are reduced to their bundle-relative tail — the absolute path can
//     contain the OS username.
//   * BEST EFFORT: every failure resolves to null. A diagnostic must never be
//     the reason the app stays broken.

import {
  HANG_STACK_MAX_FRAMES,
  sanitizeHangStackFrames,
  type HangStackFrame,
} from "../shared/hang-stack";

export type { HangStackFrame };

/** Minimal CDP debugger surface — matches Electron's `webContents.debugger`. */
export interface CdpDebugger {
  isAttached(): boolean;
  attach(protocolVersion?: string): void;
  detach(): void;
  sendCommand(method: string, commandParams?: Record<string, unknown>): Promise<unknown>;
  on(
    event: "message",
    listener: (event: unknown, method: string, params: Record<string, unknown>) => void,
  ): unknown;
  off(
    event: "message",
    listener: (event: unknown, method: string, params: Record<string, unknown>) => void,
  ): unknown;
}

/** The only CDP commands this module is permitted to send. */
export const ALLOWED_CDP_METHODS = [
  "Debugger.enable",
  "Debugger.disable",
  "Debugger.pause",
  "Debugger.resume",
] as const;

export interface CaptureHangStackOptions {
  /** Give up waiting for `Debugger.paused` after this long. */
  timeoutMs?: number;
  /** Deepest frames are the least useful; keep the top of the stack. */
  maxFrames?: number;
}

const DEFAULT_TIMEOUT_MS = 2000;

/**
 * Send a single CDP command, enforcing the allowlist. This is the ONLY path to
 * `debugger.sendCommand` in this module — nothing here may call `sendCommand`
 * directly. Rejects for any other method so a forbidden command fails loudly
 * in tests rather than silently exfiltrating.
 */
export function sendDebuggerCommand(
  dbg: CdpDebugger,
  method: string,
): Promise<unknown> {
  if (!(ALLOWED_CDP_METHODS as readonly string[]).includes(method)) {
    return Promise.reject(
      new Error(
        `Forbidden CDP method "${method}": hang stack capture may only send ${ALLOWED_CDP_METHODS.join(" / ")}`,
      ),
    );
  }
  return dbg.sendCommand(method);
}

/**
 * Open the debugger channel for this renderer. Must run while the renderer is
 * healthy — that is the entire point. Returns whether the channel is usable.
 */
export async function warmDebuggerChannel(dbg: CdpDebugger): Promise<boolean> {
  let attachedHere = false;
  try {
    if (!dbg.isAttached()) {
      dbg.attach("1.3");
      attachedHere = true;
    }
    await sendDebuggerCommand(dbg, "Debugger.enable");
    return true;
  } catch {
    // Another client owns the debugger, or the renderer is already gone.
    // An attach we made and could not enable has to be rolled back: reporting
    // failure while leaving the channel open would strand a debugger on a
    // renderer nothing is tracking.
    if (attachedHere) detachQuietly(dbg);
    return false;
  }
}

/**
 * Close the debugger channel. Called when the kill switch turns capture off:
 * the channel is what makes capture possible, so revoking the flag has to
 * revoke the channel too rather than merely skipping the next capture.
 */
export async function coolDebuggerChannel(dbg: CdpDebugger): Promise<void> {
  if (!dbg.isAttached()) return;
  try {
    await sendDebuggerCommand(dbg, "Debugger.disable");
  } catch {
    // Disable is a courtesy to the renderer; detach is the contract. A failed
    // disable must not leave the channel attached, or revoking the kill switch
    // would not actually revoke anything.
  } finally {
    detachQuietly(dbg);
  }
}

function detachQuietly(dbg: CdpDebugger): void {
  try {
    dbg.detach();
  } catch {
    // Already detached / renderer gone.
  }
}

/**
 * Interrupt the hung renderer, read the JS call stack, and let it run again.
 *
 * Resume is unconditional: a pause we requested and failed to clear would turn
 * a recoverable hang into a permanent one, which is far worse than having no
 * stack. Returns null whenever a stack could not be obtained.
 */
export async function captureHangStack(
  dbg: CdpDebugger,
  options: CaptureHangStackOptions = {},
): Promise<HangStackFrame[] | null> {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const maxFrames = options.maxFrames ?? HANG_STACK_MAX_FRAMES;

  if (!dbg.isAttached()) return null;

  let onMessage:
    | ((event: unknown, method: string, params: Record<string, unknown>) => void)
    | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;

  try {
    const paused = new Promise<HangStackFrame[] | null>((resolve) => {
      onMessage = (_event, method, params) => {
        if (method !== "Debugger.paused") return;
        resolve(sanitizeHangStackFrames(params.callFrames, maxFrames));
      };
      dbg.on("message", onMessage);
      timer = setTimeout(() => resolve(null), timeoutMs);
    });

    await sendDebuggerCommand(dbg, "Debugger.pause");
    return await paused;
  } catch {
    return null;
  } finally {
    if (timer) clearTimeout(timer);
    if (onMessage) {
      try {
        dbg.off("message", onMessage);
      } catch {
        // Listener cleanup is best-effort.
      }
    }
    try {
      await sendDebuggerCommand(dbg, "Debugger.resume");
    } catch {
      // The renderer may already be gone; nothing left to resume.
    }
  }
}
