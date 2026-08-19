import type { NotificationGroupKey, NotificationPreferences } from "../types";

export type ContentNotificationGroupKey =
  | "needs_attention"
  | "task_agent_progress"
  | "comments_mentions"
  | "system_health";

export type NotificationGroupState = "all" | "muted" | "mixed";

export interface ContentNotificationGroup {
  key: ContentNotificationGroupKey;
  legacyKeys: readonly NotificationGroupKey[];
}

export const CONTENT_NOTIFICATION_GROUPS: readonly ContentNotificationGroup[] =
  [
    { key: "needs_attention", legacyKeys: ["assignments"] },
    {
      key: "task_agent_progress",
      legacyKeys: ["status_changes", "updates", "agent_activity"],
    },
    {
      key: "comments_mentions",
      legacyKeys: ["comments", "mentions"],
    },
    { key: "system_health", legacyKeys: ["agent_activity"] },
  ];

const GROUP_BY_KEY = new Map(
  CONTENT_NOTIFICATION_GROUPS.map((group) => [group.key, group]),
);

function valueOf(
  preferences: NotificationPreferences,
  key: NotificationGroupKey,
) {
  return preferences[key] ?? "all";
}

/**
 * A canonical value wins once the user changes the collapsed group. Before
 * that, report mixed legacy choices without rewriting them behind their back.
 */
export function notificationContentGroupState(
  preferences: NotificationPreferences,
  key: ContentNotificationGroupKey,
): NotificationGroupState {
  const canonical = preferences[key];
  if (canonical) return canonical;

  const legacyKeys = GROUP_BY_KEY.get(key)?.legacyKeys ?? [];
  const values = new Set(
    legacyKeys.map((legacyKey) => valueOf(preferences, legacyKey)),
  );
  if (values.size > 1) return "mixed";
  return values.has("muted") ? "muted" : "all";
}

export function setNotificationContentGroup(
  preferences: NotificationPreferences,
  key: ContentNotificationGroupKey,
  enabled: boolean,
): NotificationPreferences {
  return { ...preferences, [key]: enabled ? "all" : "muted" };
}

export function notificationDeliveryEnabled(
  preferences: NotificationPreferences,
): boolean {
  return (
    (preferences.browser_push ?? preferences.system_notifications ?? "all") !==
    "muted"
  );
}

export function setNotificationDelivery(
  preferences: NotificationPreferences,
  enabled: boolean,
): NotificationPreferences {
  return { ...preferences, browser_push: enabled ? "all" : "muted" };
}
