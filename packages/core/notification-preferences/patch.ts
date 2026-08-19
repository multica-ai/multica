import type {
  NotificationGroupKey,
  NotificationGroupValue,
  NotificationPreferences,
} from "../types";

const NOTIFICATION_GROUP_KEYS: readonly NotificationGroupKey[] = [
  "needs_attention",
  "task_agent_progress",
  "comments_mentions",
  "system_health",
  "browser_push",
  "assignments",
  "status_changes",
  "comments",
  "mentions",
  "updates",
  "agent_activity",
  "system_notifications",
];

const CANONICAL_GROUP_KEYS = new Set<NotificationGroupKey>([
  "needs_attention",
  "task_agent_progress",
  "comments_mentions",
  "system_health",
  "browser_push",
]);

function preferenceValue(
  preferences: NotificationPreferences,
  key: NotificationGroupKey,
): NotificationGroupValue {
  return preferences[key] ?? "all";
}

/**
 * Convert the full preference object produced by the settings UI into the
 * smallest atomic patch. Missing keys mean the default value ("all").
 */
export function deriveNotificationPreferencePatch(
  previous: NotificationPreferences,
  next: NotificationPreferences,
): NotificationPreferences {
  const patch: NotificationPreferences = {};

  for (const key of NOTIFICATION_GROUP_KEYS) {
    const previousValue = CANONICAL_GROUP_KEYS.has(key)
      ? previous[key]
      : preferenceValue(previous, key);
    const nextValue = CANONICAL_GROUP_KEYS.has(key)
      ? next[key]
      : preferenceValue(next, key);
    if (previousValue !== nextValue) {
      patch[key] = nextValue;
    }
  }

  return patch;
}

/** Apply a preference patch while keeping default "all" values sparse. */
export function applyNotificationPreferencePatch(
  current: NotificationPreferences,
  patch: NotificationPreferences,
): NotificationPreferences {
  const next = { ...current };

  for (const key of NOTIFICATION_GROUP_KEYS) {
    const value = patch[key];
    if (
      value === "muted" ||
      (value === "all" && CANONICAL_GROUP_KEYS.has(key))
    ) {
      next[key] = value;
    } else if (value === "all") {
      delete next[key];
    }
  }

  return next;
}

/**
 * Roll back only values that still match this mutation's optimistic patch.
 * A later toggle that touched the same key wins.
 */
export function rollbackNotificationPreferencePatch(
  current: NotificationPreferences,
  patch: NotificationPreferences,
  previous: NotificationPreferences,
): NotificationPreferences {
  const next = { ...current };

  for (const key of NOTIFICATION_GROUP_KEYS) {
    const patchedValue = patch[key];
    if (
      patchedValue === undefined ||
      preferenceValue(current, key) !== patchedValue
    ) {
      continue;
    }

    const previousValue = previous[key];
    if (previousValue === undefined) {
      delete next[key];
    } else {
      next[key] = previousValue;
    }
  }

  return next;
}
