import type { InboxItemType } from "../types";

// Route destinations the user can pick per notification type.
//   - "inbox"          → persistent inbox queue (action_required by default)
//   - "notifications"  → lightweight notifications page in the sidebar bottom
//   - "off"            → don't create the item at all
// Server keeps the source of truth; client mirrors the contract for UI.
export type RouteChoice = "inbox" | "notifications" | "off";

// Notification types that are emitted by the server today. The TS InboxItemType
// union also lists future types (review_requested, agent_blocked, etc.) but the
// settings UI should only show types we actually deliver.
export type EmittedNotificationType = Extract<
  InboxItemType,
  | "issue_assigned"
  | "mentioned"
  | "task_failed"
  | "unassigned"
  | "reaction_added"
  | "new_comment"
  | "assignee_changed"
  | "status_changed"
  | "priority_changed"
  | "due_date_changed"
>;

// Two types route differently depending on whether the recipient is the
// assignee on the issue ("mine") or just a subscriber ("following").
// Keep this in sync with `splitTypes` in server/cmd/server/notification_routing.go.
export const SPLIT_TYPES: ReadonlySet<EmittedNotificationType> = new Set([
  "due_date_changed",
  "priority_changed",
]);

// Routing key used inside `user.preferences.notifications`. Keep in sync with
// `routeKey` in server/cmd/server/notification_routing.go.
export type RoutingKey =
  | "issue_assigned"
  | "mentioned"
  | "task_failed"
  | "unassigned"
  | "reaction_added"
  | "new_comment"
  | "assignee_changed"
  | "status_changed"
  | "due_date_changed.assignee"
  | "due_date_changed.follower"
  | "priority_changed.assignee"
  | "priority_changed.follower";

// Mirror of `defaultRoutes` on the server — used to render the segmented
// control with the right initial selection when the user has no override.
export const DEFAULT_ROUTES: Record<RoutingKey, RouteChoice> = {
  issue_assigned: "inbox",
  mentioned: "inbox",
  task_failed: "inbox",
  unassigned: "notifications",
  reaction_added: "notifications",
  new_comment: "notifications",
  assignee_changed: "notifications",
  status_changed: "notifications",
  "due_date_changed.assignee": "inbox",
  "due_date_changed.follower": "notifications",
  "priority_changed.assignee": "inbox",
  "priority_changed.follower": "notifications",
};

// Pull a user's chosen route out of the preferences blob, falling back to the
// default when the value is missing or invalid.
export function getRouteChoice(
  prefs: Record<string, unknown> | undefined,
  key: RoutingKey,
): RouteChoice {
  const block = (prefs?.notifications ?? {}) as Record<string, unknown>;
  const value = block[key];
  if (value === "inbox" || value === "notifications" || value === "off") {
    return value;
  }
  return DEFAULT_ROUTES[key];
}
