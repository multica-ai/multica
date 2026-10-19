import type { TaskFailureReason } from "../types";

/**
 * Resolve failure copy by exact reason, then coarse family, then caller fallback.
 * The server taxonomy can grow independently of installed clients; unfamiliar
 * agent_error subtypes should retain family-level guidance. Keep this helper pure
 * so mobile can share it.
 */
export function resolveFailureReasonKey<K extends string>(
  reason: TaskFailureReason | string | null | undefined,
  copy: Readonly<Partial<Record<K, unknown>>>,
): K | undefined {
  if (!reason) return undefined;
  if (Object.prototype.hasOwnProperty.call(copy, reason)) {
    return reason as K;
  }
  const separator = reason.indexOf(".");
  if (separator > 0) {
    const family = reason.slice(0, separator);
    if (Object.prototype.hasOwnProperty.call(copy, family)) {
      return family as K;
    }
  }
  return undefined;
}
