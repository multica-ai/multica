import type { NotificationPreferences } from "../types";

type PreferenceKey = keyof NotificationPreferences;
type PreferenceValues = Record<PreferenceKey, string | undefined>;

/**
 * The value each key means when it is missing. Every key the settings UI can
 * write must be listed, or its change is silently never sent. Must match the
 * server's defaults (`mutedByDefault` in server/internal/inbox/registry.go).
 */
const PREFERENCE_DEFAULTS: { [K in PreferenceKey]-?: NonNullable<NotificationPreferences[K]> } = {
  assignments: "all",
  status_changes: "muted",
  comments: "all",
  chat_replies: "all",
  mentions: "all",
  reactions: "all",
  updates: "all",
  new_issues: "all",
  agent_activity: "all",
  system_notifications: "all",
  auto_archive_completed: "all",
  others_issues: "key_updates",
};

const PREFERENCE_KEYS = Object.keys(PREFERENCE_DEFAULTS) as PreferenceKey[];

/** The effective value of a preference, filling in its default. */
export function notificationPreferenceValue<K extends PreferenceKey>(
  preferences: NotificationPreferences,
  key: K,
): NonNullable<NotificationPreferences[K]> {
  return (preferences[key] ?? PREFERENCE_DEFAULTS[key]) as NonNullable<NotificationPreferences[K]>;
}

/** Set a value on a copy, dropping the key when it is the default. */
function withValue(
  preferences: NotificationPreferences,
  key: PreferenceKey,
  value: string,
): NotificationPreferences {
  const next = { ...preferences } as PreferenceValues;
  if (value === PREFERENCE_DEFAULTS[key]) {
    delete next[key];
  } else {
    next[key] = value;
  }
  return next as NotificationPreferences;
}

/**
 * Convert the full preference object produced by the settings UI into the
 * smallest atomic patch. Missing keys mean their default value.
 */
export function deriveNotificationPreferencePatch(
  previous: NotificationPreferences,
  next: NotificationPreferences,
): NotificationPreferences {
  const patch = {} as PreferenceValues;

  for (const key of PREFERENCE_KEYS) {
    const previousValue = notificationPreferenceValue(previous, key);
    const nextValue = notificationPreferenceValue(next, key);
    if (previousValue !== nextValue) {
      patch[key] = nextValue;
    }
  }

  return patch as NotificationPreferences;
}

/** Apply a preference patch while keeping default values sparse. */
export function applyNotificationPreferencePatch(
  current: NotificationPreferences,
  patch: NotificationPreferences,
): NotificationPreferences {
  let next = { ...current };

  for (const key of PREFERENCE_KEYS) {
    const value = patch[key];
    if (value !== undefined) {
      next = withValue(next, key, value);
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
  let next = { ...current };

  for (const key of PREFERENCE_KEYS) {
    const patchedValue = patch[key];
    if (
      patchedValue === undefined ||
      notificationPreferenceValue(current, key) !== patchedValue
    ) {
      continue;
    }
    next = withValue(next, key, notificationPreferenceValue(previous, key));
  }

  return next;
}
