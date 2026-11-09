export type NotificationGroupKey =
  | "assignments"
  | "status_changes"
  | "comments"
  | "mentions"
  | "updates"
  | "agent_activity"
  | "system_notifications";

export type NotificationGroupValue = "all" | "muted";

/**
 * "on": replying, changing the status or reassigning archives the member's
 * Inbox rows on that issue. Absent or "off", they archive rows themselves.
 */
export type AutoArchiveValue = "on" | "off";

export type NotificationPreferences = Partial<Record<NotificationGroupKey, NotificationGroupValue>> & {
  auto_archive?: AutoArchiveValue;
};

export interface NotificationPreferenceResponse {
  workspace_id: string;
  preferences: NotificationPreferences;
}
