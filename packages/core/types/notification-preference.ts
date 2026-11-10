export type NotificationGroupKey =
  | "assignments"
  | "status_changes"
  | "comments"
  | "chat_replies"
  | "mentions"
  | "updates"
  | "new_issues"
  | "agent_activity"
  | "system_notifications";

export type NotificationGroupValue = "all" | "muted";

export type InboxMode = "all" | "focused";

export type FocusedNotificationKey =
  | "focused_assigned"
  | "focused_blocked"
  | "focused_mentioned"
  | "focused_thread_reply"
  | "focused_agent_activity";

export type FocusedNotificationValue = "on" | "off";

export type NotificationPreferences = Partial<
  Record<NotificationGroupKey, NotificationGroupValue> &
    Record<FocusedNotificationKey, FocusedNotificationValue> & {
      inbox_mode: InboxMode;
    }
>;

export type NotificationPreferenceKey = keyof NotificationPreferences;

export interface NotificationPreferenceResponse {
  workspace_id: string;
  preferences: NotificationPreferences;
}
