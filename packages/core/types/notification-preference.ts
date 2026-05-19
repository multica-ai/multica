export type NotificationGroupKey =
  | "assignments"
  | "status_changes"
  | "comments"
  | "updates"
  | "agent_activity"
  | "system_notifications";

export type NotificationGroupValue = "all" | "muted";

/**
 * Channel connection preferences. Each key represents one event family a
 * concrete channel connection can mute.
 *
 * Default semantics: a key absent from this object is treated as
 * **enabled** (default-on). Only an explicit `false` mutes the family.
 * The backend (`server/internal/handler/notification_preference.go`)
 * holds the same contract — see `IsChannelEventEnabled` for the
 * canonical predicate. Use {@link isChannelEventEnabled} below from
 * frontend code rather than re-implementing the rule.
 */
export interface ChannelNotificationPreferences {
  issues?: boolean;
  comments?: boolean;
  mentions?: boolean;
  slash_aliases?: Record<string, string>;
}

export type ChannelEventKey = "issues" | "comments" | "mentions";

export type ChannelPreferences = Record<string, ChannelNotificationPreferences | undefined>;

export interface NotificationPreferences {
  assignments?: NotificationGroupValue;
  status_changes?: NotificationGroupValue;
  comments?: NotificationGroupValue;
  updates?: NotificationGroupValue;
  agent_activity?: NotificationGroupValue;
  system_notifications?: NotificationGroupValue;
  channel?: ChannelPreferences;
}

export interface NotificationPreferenceResponse {
  workspace_id: string;
  preferences: NotificationPreferences;
}

/**
 * Returns true when a concrete channel connection should deliver an event of
 * the given key for the given preferences. Missing keys mean
 * "enabled" (default-on); explicit false means muted.
 *
 * Centralising this rule keeps the UI and any future frontend consumer
 * aligned with the backend default semantics.
 */
export function isChannelEventEnabled(
  prefs: NotificationPreferences | undefined | null,
  connectionId: string,
  key: ChannelEventKey,
): boolean {
  const value = prefs?.channel?.[connectionId]?.[key];
  return value !== false;
}
