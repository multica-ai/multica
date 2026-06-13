export type NotificationGroupKey =
  | "assignments"
  | "status_changes"
  | "comments"
  | "updates"
  | "agent_activity"
  | "system_notifications"
  | "email_notifications";

export type NotificationGroupValue = "all" | "muted";

export type NotificationPreferences = Partial<Record<NotificationGroupKey, NotificationGroupValue>>;

export interface NotificationPreferenceResponse {
  workspace_id: string;
  preferences: NotificationPreferences;
}
