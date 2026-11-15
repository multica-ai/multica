import type {
  Agent,
  IssueAssigneeType,
  IssueSubscriber,
  NotificationPreferences,
} from "../types";
import { notificationPreferenceValue } from "./patch";

/**
 * Subscriptions picked up as a side effect of acting on an issue. manual and
 * autopilot are explicit and keep every event; delegated has its own tier.
 */
const BYSTANDER_REASONS: ReadonlySet<IssueSubscriber["reason"]> = new Set([
  "creator",
  "assignee",
  "commenter",
  "mentioned",
]);

/**
 * Whether the server delivers only key updates of this issue to this member:
 * a passive subscription, on an issue assigned to someone other than them or
 * an agent they own, with "Other people's issues" left at key updates, and
 * no subscription to the issue's project (that path delivers every event).
 * Mirrors deliverToBystander in server/cmd/server/notification_listeners.go.
 */
export function receivesKeyUpdatesOnly({
  reason,
  projectCovered = false,
  issue,
  userId,
  agents,
  preferences,
}: {
  reason: IssueSubscriber["reason"] | undefined;
  projectCovered?: boolean;
  issue: { assignee_type?: IssueAssigneeType | null; assignee_id?: string | null };
  userId: string | null | undefined;
  agents: readonly Pick<Agent, "id" | "owner_id">[];
  preferences: NotificationPreferences;
}): boolean {
  if (!userId || !reason || !BYSTANDER_REASONS.has(reason)) return false;
  if (projectCovered) return false;
  if (notificationPreferenceValue(preferences, "others_issues") === "all") return false;
  if (!issue.assignee_id) return false;
  switch (issue.assignee_type) {
    case "member":
      return issue.assignee_id !== userId;
    case "agent":
      return !agents.some((a) => a.id === issue.assignee_id && a.owner_id === userId);
    default:
      return true;
  }
}
