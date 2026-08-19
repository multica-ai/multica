export type NotificationGroupKey =
  | "assignments"
  | "status_changes"
  | "comments"
  | "mentions"
  | "updates"
  | "agent_activity"
  | "needs_attention"
  | "task_agent_progress"
  | "comments_mentions"
  | "system_health"
  | "browser_push"
  | "system_notifications";

export type NotificationGroupValue = "all" | "muted";

export type NotificationPreferences = Partial<Record<NotificationGroupKey, NotificationGroupValue>>;

export interface NotificationPreferenceResponse {
  workspace_id: string;
  preferences: NotificationPreferences;
}

export interface WebPushConfigResponse {
  enabled: boolean;
  public_key: string;
}

export interface WebPushSubscriptionRequest {
  endpoint: string;
  expirationTime: number | null;
  keys: { p256dh: string; auth: string };
}
