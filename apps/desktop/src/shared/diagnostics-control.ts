// Renderer → main push of the diagnostics gate for on-hang CPU profiling
// (MUL-3738). The renderer owns the two signals that decide whether the main
// process may sample a hung renderer:
//
//   1. cpuProfileEnabled — the backend `/api/config` feature flag
//      (`diagnostics_cpu_profile_enabled`, default false).
//   2. optOut — the analytics opt-out state (posthog
//      `has_opted_out_capturing()`, treated true when analytics isn't running).
//
// Both live in the renderer, but the profile is captured by the main process
// at hang time — when it can no longer ask the (frozen) renderer anything. So
// the renderer PRE-PUSHES this state; main holds the last-known value and
// captures only when `cpuProfileEnabled === true && optOut === false`. A hang
// before the first push leaves main with no control object, which it treats as
// "do not capture" (see main/index.ts).

export const DIAGNOSTICS_CONTROL_CHANNEL = "renderer:diagnostics-control";

export interface DiagnosticsControl {
  /** Backend feature flag — main never captures a profile when this is false. */
  cpuProfileEnabled: boolean;
  /** Analytics opt-out — main never captures a profile when this is true. */
  optOut: boolean;
}

/**
 * Validate an IPC-delivered control payload. Returns null for any non-
 * conforming shape so a malformed push can't flip the gate open.
 */
export function sanitizeDiagnosticsControl(value: unknown): DiagnosticsControl | null {
  if (!value || typeof value !== "object") return null;
  const input = value as Record<string, unknown>;
  if (typeof input.cpuProfileEnabled !== "boolean") return null;
  if (typeof input.optOut !== "boolean") return null;
  return {
    cpuProfileEnabled: input.cpuProfileEnabled,
    optOut: input.optOut,
  };
}
