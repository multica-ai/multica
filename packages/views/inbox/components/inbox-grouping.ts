import type { InboxItem } from "@multica/core/types";
import type { GroupMode } from "@multica/core/inbox/inbox-filter-store";

export interface InboxGroup {
  key: string;
  label: string;
  items: InboxItem[];
  unreadCount: number;
  totalCount: number;
}

function isToday(dateStr: string): boolean {
  const d = new Date(dateStr);
  const now = new Date();
  return (
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  );
}

function isYesterday(dateStr: string): boolean {
  const d = new Date(dateStr);
  const yesterday = new Date();
  yesterday.setDate(yesterday.getDate() - 1);
  return (
    d.getFullYear() === yesterday.getFullYear() &&
    d.getMonth() === yesterday.getMonth() &&
    d.getDate() === yesterday.getDate()
  );
}

function isThisWeek(dateStr: string): boolean {
  const d = new Date(dateStr);
  const now = new Date();
  const startOfWeek = new Date(now);
  startOfWeek.setDate(now.getDate() - now.getDay());
  startOfWeek.setHours(0, 0, 0, 0);
  const startOfYesterday = new Date(now);
  startOfYesterday.setDate(now.getDate() - 1);
  startOfYesterday.setHours(0, 0, 0, 0);
  // "This week" means from start of week until before yesterday
  return d >= startOfWeek && d < startOfYesterday;
}

export function groupByTime(items: InboxItem[]): InboxGroup[] {
  const today: InboxItem[] = [];
  const yesterday: InboxItem[] = [];
  const thisWeek: InboxItem[] = [];
  const older: InboxItem[] = [];

  for (const item of items) {
    if (isToday(item.created_at)) {
      today.push(item);
    } else if (isYesterday(item.created_at)) {
      yesterday.push(item);
    } else if (isThisWeek(item.created_at)) {
      thisWeek.push(item);
    } else {
      older.push(item);
    }
  }

  const result: InboxGroup[] = [];
  const order: { key: string; label: string; items: InboxItem[] }[] = [
    { key: "today", label: "Today", items: today },
    { key: "yesterday", label: "Yesterday", items: yesterday },
    { key: "this_week", label: "This week", items: thisWeek },
    { key: "older", label: "Older", items: older },
  ];

  for (const { key, label, items: grpItems } of order) {
    if (grpItems.length > 0) {
      result.push({
        key,
        label,
        items: grpItems,
        unreadCount: grpItems.filter((i) => !i.read).length,
        totalCount: grpItems.length,
      });
    }
  }

  return result;
}

export function groupBySeverity(items: InboxItem[]): InboxGroup[] {
  const actionRequired: InboxItem[] = [];
  const attention: InboxItem[] = [];
  const info: InboxItem[] = [];

  for (const item of items) {
    const sev = item.severity ?? "info";
    if (sev === "action_required") {
      actionRequired.push(item);
    } else if (sev === "attention") {
      attention.push(item);
    } else {
      info.push(item);
    }
  }

  const result: InboxGroup[] = [];
  const order: { key: string; label: string; items: InboxItem[] }[] = [
    { key: "action_required", label: "Action Required", items: actionRequired },
    { key: "attention", label: "Attention", items: attention },
    { key: "info", label: "Info", items: info },
  ];

  for (const { key, label, items: grpItems } of order) {
    if (grpItems.length > 0) {
      result.push({
        key,
        label,
        items: grpItems,
        unreadCount: grpItems.filter((i) => !i.read).length,
        totalCount: grpItems.length,
      });
    }
  }

  return result;
}

export function groupByProject(items: InboxItem[]): InboxGroup[] {
  const groups = new Map<string, InboxItem[]>();

  for (const item of items) {
    const projectName = item.details?.project_name?.trim();
    const key = projectName || "__no_project__";
    const existing = groups.get(key) ?? [];
    existing.push(item);
    groups.set(key, existing);
  }

  const result: InboxGroup[] = [];
  const noProjectItems = groups.get("__no_project__");
  groups.delete("__no_project__");

  // Sort named projects alphabetically
  const sortedKeys = Array.from(groups.keys()).sort((a, b) =>
    a.localeCompare(b),
  );

  for (const key of sortedKeys) {
    const groupItems = groups.get(key)!;
    result.push({
      key: `project:${key}`,
      label: key,
      items: groupItems,
      unreadCount: groupItems.filter((i) => !i.read).length,
      totalCount: groupItems.length,
    });
  }

  // "No project" catch-all goes last
  if (noProjectItems && noProjectItems.length > 0) {
    result.push({
      key: "project:__no_project__",
      label: "No project",
      items: noProjectItems,
      unreadCount: noProjectItems.filter((i) => !i.read).length,
      totalCount: noProjectItems.length,
    });
  }

  return result;
}

export function groupByType(items: InboxItem[]): InboxGroup[] {
  const groups = new Map<string, InboxItem[]>();

  for (const item of items) {
    const key = item.type;
    const existing = groups.get(key) ?? [];
    existing.push(item);
    groups.set(key, existing);
  }

  // Define type group categories with labels
  const typeCategories: Record<string, { label: string; types: string[] }> = {
    comments: {
      label: "Comments",
      types: ["new_comment", "mentioned", "reaction_added"],
    },
    assignments: {
      label: "Assignments",
      types: [
        "issue_assigned",
        "issue_subscribed",
        "unassigned",
        "assignee_changed",
        "review_requested",
      ],
    },
    status_changes: {
      label: "Status changes",
      types: [
        "status_changed",
        "priority_changed",
        "start_date_changed",
        "due_date_changed",
      ],
    },
    agents: {
      label: "Agents",
      types: [
        "task_completed",
        "task_failed",
        "agent_blocked",
        "agent_completed",
        "quick_create_done",
        "quick_create_failed",
      ],
    },
  };

  const result: InboxGroup[] = [];

  for (const [catKey, { label, types }] of Object.entries(typeCategories)) {
    const catItems: InboxItem[] = [];
    for (const type of types) {
      const typeItems = groups.get(type);
      if (typeItems) {
        catItems.push(...typeItems);
      }
    }
    if (catItems.length > 0) {
      result.push({
        key: `type:${catKey}`,
        label,
        items: catItems,
        unreadCount: catItems.filter((i) => !i.read).length,
        totalCount: catItems.length,
      });
    }
  }

  return result;
}

export function groupInboxItems(
  items: InboxItem[],
  mode: GroupMode,
): InboxGroup[] {
  switch (mode) {
    case "time":
      return groupByTime(items);
    case "severity":
      return groupBySeverity(items);
    case "project":
      return groupByProject(items);
    case "type":
      return groupByType(items);
    default:
      return groupByTime(items);
  }
}
