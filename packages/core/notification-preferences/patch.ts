import type { NotificationPreferences } from "../types";

type PreferenceKey = keyof NotificationPreferences;

/** Each key's value when absent. The cache keeps defaults absent. */
const DEFAULTS: { [K in PreferenceKey]-?: NonNullable<NotificationPreferences[K]> } = {
  assignments: "all",
  status_changes: "all",
  comments: "all",
  mentions: "all",
  updates: "all",
  agent_activity: "all",
  system_notifications: "all",
  auto_archive: "off",
};

const PREFERENCE_KEYS = Object.keys(DEFAULTS) as PreferenceKey[];

function preferenceValue(preferences: NotificationPreferences, key: PreferenceKey): string {
  return preferences[key] ?? DEFAULTS[key];
}

function setValue(preferences: NotificationPreferences, key: PreferenceKey, value: string) {
  if (value === DEFAULTS[key]) {
    delete preferences[key];
  } else {
    (preferences as Record<PreferenceKey, string>)[key] = value;
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

  for (const key of PREFERENCE_KEYS) {
    const previousValue = preferenceValue(previous, key);
    const nextValue = preferenceValue(next, key);
    if (previousValue !== nextValue) {
      (patch as Record<PreferenceKey, string>)[key] = nextValue;
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

  for (const key of PREFERENCE_KEYS) {
    const value = patch[key];
    if (value !== undefined) setValue(next, key, value);
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

  for (const key of PREFERENCE_KEYS) {
    const patchedValue = patch[key];
    if (
      patchedValue === undefined ||
      preferenceValue(current, key) !== patchedValue
    ) {
      continue;
    }

    setValue(next, key, preferenceValue(previous, key));
  }

  return next;
}
