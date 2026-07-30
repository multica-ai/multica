import {
  parseDiagnosticsControl,
  type DiagnosticsControl,
} from "../shared/diagnostics-control";

/**
 * Which renderers are allowed to be interrogated, tracked per renderer.
 *
 * A single global flag looked right — the value is one server-side switch, so
 * every renderer reports the same thing — but it does not survive multiple
 * windows. Every renderer publishes `false` before its config arrives, so a
 * window opened while capture was already on would either be skipped (the
 * global value never changed, so nothing warmed it) or, worse, would revoke
 * capture for every other window on the way up. Windows come and go
 * independently, so the state has to be per renderer.
 *
 * Each renderer therefore owns its own entry: its report warms or cools its
 * own channel and nothing else. They converge on the same value because they
 * read the same config, but they get there on their own schedules.
 */
export interface DiagnosticsControlRegistry<T> {
  /** Apply a raw control payload from one renderer. Fail-closed on garbage. */
  apply: (target: T, rawControl: unknown) => void;
  /** Whether this renderer may currently be interrogated. */
  isStackCaptureEnabled: (target: T) => boolean;
}

export interface DiagnosticsControlHandlers<T> {
  /** Open the debugger channel for this renderer. */
  warm: (target: T) => void;
  /** Close the debugger channel for this renderer. */
  cool: (target: T) => void;
}

export function createDiagnosticsControlRegistry<T extends object>({
  warm,
  cool,
}: DiagnosticsControlHandlers<T>): DiagnosticsControlRegistry<T> {
  // Keyed by the renderer itself, so a closed window's entry disappears with
  // it rather than pinning the object alive.
  const controls = new WeakMap<T, DiagnosticsControl>();

  const enabled = (target: T) =>
    controls.get(target)?.stackCaptureEnabled === true;

  return {
    apply(target, rawControl) {
      const next = parseDiagnosticsControl(rawControl);
      const was = enabled(target);
      controls.set(target, next);
      if (next.stackCaptureEnabled === was) return;
      if (next.stackCaptureEnabled) warm(target);
      else cool(target);
    },

    isStackCaptureEnabled: enabled,
  };
}
