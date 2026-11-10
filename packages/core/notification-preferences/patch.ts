import type {
  NotificationPreferenceKey,
  NotificationPreferences,
} from "../types/notification-preference";

// Every key the client diffs, with the value an absent key stands for.
// Default values are kept sparse: a patch to the default deletes the key.
const NOTIFICATION_PREFERENCE_DEFAULTS = {
  assignments: "all",
  status_changes: "all",
  comments: "all",
  chat_replies: "all",
  mentions: "all",
  updates: "all",
  new_issues: "all",
  agent_activity: "all",
  system_notifications: "all",
  inbox_mode: "all",
  focused_assigned: "on",
  focused_blocked: "on",
  focused_mentioned: "on",
  focused_thread_reply: "on",
  focused_agent_activity: "on",
} as const satisfies Required<NotificationPreferences>;

const NOTIFICATION_PREFERENCE_KEYS = Object.keys(
  NOTIFICATION_PREFERENCE_DEFAULTS,
) as NotificationPreferenceKey[];

function preferenceValue(
  preferences: NotificationPreferences,
  key: NotificationPreferenceKey,
): string {
  return preferences[key] ?? NOTIFICATION_PREFERENCE_DEFAULTS[key];
}

function setPreference(
  preferences: NotificationPreferences,
  key: NotificationPreferenceKey,
  value: string,
) {
  if (value === NOTIFICATION_PREFERENCE_DEFAULTS[key]) {
    delete preferences[key];
  } else {
    (preferences as Record<string, string>)[key] = value;
  }
}

/**
 * Convert the full preference object produced by the settings UI into the
 * smallest atomic patch. Missing keys mean the key's default value.
 */
export function deriveNotificationPreferencePatch(
  previous: NotificationPreferences,
  next: NotificationPreferences,
): NotificationPreferences {
  const patch: NotificationPreferences = {};

  for (const key of NOTIFICATION_PREFERENCE_KEYS) {
    const previousValue = preferenceValue(previous, key);
    const nextValue = preferenceValue(next, key);
    if (previousValue !== nextValue) {
      (patch as Record<string, string>)[key] = nextValue;
    }
  }

  return patch;
}

/** Apply a preference patch while keeping default values sparse. */
export function applyNotificationPreferencePatch(
  current: NotificationPreferences,
  patch: NotificationPreferences,
): NotificationPreferences {
  const next = { ...current };

  for (const key of NOTIFICATION_PREFERENCE_KEYS) {
    const value = patch[key];
    if (value !== undefined) setPreference(next, key, value);
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

  for (const key of NOTIFICATION_PREFERENCE_KEYS) {
    const patchedValue = patch[key];
    if (
      patchedValue === undefined ||
      preferenceValue(current, key) !== patchedValue
    ) {
      continue;
    }

    setPreference(next, key, preferenceValue(previous, key));
  }

  return next;
}
