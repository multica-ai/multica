export const DIAGNOSTICS_CONTROL_CHANNEL = "diagnostics:control";

/**
 * Server-driven feature flag that gates hang stack capture.
 *
 * Lives in `/api/config`'s `feature_flags`, so it can be flipped off for the
 * whole fleet without shipping a release — which is the point: attaching a
 * debugger to every renderer is the kind of thing that must be revocable
 * faster than an app update can roll out.
 */
export const HANG_STACK_CAPTURE_FLAG = "desktop_hang_stack_capture";

export interface DiagnosticsControl {
  /** Whether the main process may hold a debugger channel and read stacks. */
  stackCaptureEnabled: boolean;
}

/** What the main process assumes until the renderer says otherwise. */
export const DIAGNOSTICS_CONTROL_OFF: DiagnosticsControl = {
  stackCaptureEnabled: false,
};

/**
 * Read a control payload sent by the renderer, FAIL-CLOSED.
 *
 * Anything that is not literally `true` means off: a malformed payload, a
 * renderer older than this build, a flag the backend has never heard of, a
 * config request that failed. The only way capture turns on is an explicit
 * `true` from a config the client actually received.
 */
export function parseDiagnosticsControl(value: unknown): DiagnosticsControl {
  if (!value || typeof value !== "object") return DIAGNOSTICS_CONTROL_OFF;
  const input = value as Record<string, unknown>;
  return { stackCaptureEnabled: input.stackCaptureEnabled === true };
}
