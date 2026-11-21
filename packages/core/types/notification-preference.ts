export type NotificationGroupKey =
  | "assignments"
  | "status_changes"
  | "comments"
  | "chat_replies"
  | "mentions"
  | "reactions"
  | "updates"
  | "new_issues"
  | "agent_activity"
  | "system_notifications"
  | "auto_archive_completed"
  | "auto_archive_on_action";

export type NotificationGroupValue = "all" | "muted";

/**
 * How much of someone else's issue reaches you when you are only passively
 * subscribed (you filed it, commented, or were mentioned). Missing means
 * "key_updates": mentions, replies in your threads and handoffs.
 */
export type OthersIssuesValue = "all" | "key_updates";

/**
 * Where the Inbox goes when the notification you are reading is archived
 * because you handled its issue. Missing means "stay".
 */
export type AfterAutoArchiveValue = "stay" | "next";

export type NotificationPreferences = Partial<Record<NotificationGroupKey, NotificationGroupValue>> & {
  others_issues?: OthersIssuesValue;
  after_auto_archive?: AfterAutoArchiveValue;
};

export interface NotificationPreferenceResponse {
  workspace_id: string;
  preferences: NotificationPreferences;
}
