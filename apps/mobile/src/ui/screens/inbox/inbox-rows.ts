import type { InboxItem } from "@multica/core/types";
import type { ActorResolver } from "../../model/people";
import { relativeTime, type Strings } from "../../strings";
import type { InboxBadge, InboxRow } from "./InboxView";

function badgeFor(item: InboxItem): InboxBadge | null {
  const to = item.details?.to;
  switch (item.type) {
    case "new_comment":
      return { kind: "icon", icon: "Quote" };
    case "mentioned":
      return { kind: "icon", icon: "AtSign" };
    case "issue_assigned":
    case "unassigned":
    case "assignee_changed":
      return { kind: "icon", icon: "CircleUser" };
    case "start_date_changed":
    case "due_date_changed":
      return { kind: "icon", icon: "Calendar" };
    case "task_failed":
    case "quick_create_failed":
      return { kind: "icon", icon: "AlertCircle" };
    case "status_changed":
      return to ? { kind: "status", status: to } : null;
    case "review_requested":
      return { kind: "status", status: "in_review" };
    default:
      return item.issue_status ? { kind: "status", status: item.issue_status } : null;
  }
}

function verbFor(item: InboxItem, strings: Strings, statusLabel: (key: string) => string): string {
  const verbs = strings.inbox.verbs;
  if (item.type === "status_changed" && item.details?.to) return verbs.statusChanged(statusLabel(item.details.to));
  return verbs[item.type as keyof typeof verbs] as string | undefined ?? verbs.fallback;
}

export function inboxRows(items: readonly InboxItem[], resolveActor: ActorResolver, statusLabel: (key: string) => string, strings: Strings, now = Date.now()): InboxRow[] {
  return items.map((item) => {
    const actor = resolveActor(item.actor_type, item.actor_id);
    const body = item.body?.replace(/\s+/g, " ").trim() || null;
    return {
      id: item.id,
      issueId: item.issue_id,
      identifier: item.details?.identifier ?? null,
      title: item.title,
      summary: strings.inbox.summary(actor.name, verbFor(item, strings, statusLabel), body),
      time: relativeTime(item.created_at, strings, now),
      actor,
      badge: badgeFor(item),
      unread: !item.read,
    };
  });
}
