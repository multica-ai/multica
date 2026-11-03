/** A kind of notification whose Inbox section the recipient chooses. */
export type NotificationRouteKey =
  | "assigned"
  | "mentions"
  | "comments_without_mentions"
  | "agent_failures"
  | "status_changes"
  | "assignee_changes"
  | "updates"
  | "comments_mentioning_others";

/** Where a routable kind lands: an Inbox section, or nowhere. */
export type NotificationRoute = "action" | "activity" | "muted";

/** Groups from before per-kind routing; only their "muted" still applies. */
export type NotificationLegacyGroupKey = "assignments" | "comments" | "agent_activity";

export type NotificationGroupKey =
  | NotificationRouteKey
  | NotificationLegacyGroupKey
  | "system_notifications";

/** "all" (or an absent key) means the kind's default. */
export type NotificationGroupValue = "all" | NotificationRoute;

export type NotificationPreferences = Partial<Record<NotificationGroupKey, NotificationGroupValue>>;

export interface NotificationPreferenceResponse {
  workspace_id: string;
  preferences: NotificationPreferences;
}
