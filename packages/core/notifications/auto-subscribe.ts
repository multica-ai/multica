// Auto-subscribe preferences — control whether the server adds you as an
// issue subscriber when you trigger one of these events. Mirror of
// `autoSubscribeDefaults` in server/cmd/server/subscriber_listeners.go.

export type AutoSubscribeReason =
  | "creator"
  | "assignee"
  | "mentioned"
  | "commenter";

export const AUTO_SUBSCRIBE_DEFAULTS: Record<AutoSubscribeReason, boolean> = {
  creator: true,
  assignee: true,
  mentioned: false,
  commenter: false,
};

// Reads the effective on/off for a reason. Falls back to the default when the
// preference is missing or malformed.
export function getAutoSubscribe(
  prefs: Record<string, unknown> | undefined,
  reason: AutoSubscribeReason,
): boolean {
  const block = (prefs?.auto_subscribe ?? {}) as Record<string, unknown>;
  const value = block[reason];
  if (typeof value === "boolean") {
    return value;
  }
  return AUTO_SUBSCRIBE_DEFAULTS[reason];
}
