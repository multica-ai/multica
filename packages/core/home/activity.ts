import type { InboxItem } from "../types";

/** One issue's worth of inbox activity since the viewer's last Home visit. */
export interface ActivityGroup {
  /** `issue_id`, or the item id for an issue-less notification. */
  key: string;
  issueId: string | null;
  /** Newest first. */
  items: InboxItem[];
  latest: InboxItem;
  unreadCount: number;
}

/**
 * Folds the unarchived inbox into per-issue groups of what arrived after
 * `since`, newest group first. This is Home's "by issue" view: the same events
 * the activity list shows, read as "which issues moved" instead of "what
 * happened, in order".
 */
export function groupActivityByIssue(
  items: readonly InboxItem[],
  since: string,
): ActivityGroup[] {
  const boundary = new Date(since).getTime();
  const groups = new Map<string, InboxItem[]>();
  for (const item of items) {
    if (item.archived) continue;
    if (new Date(item.created_at).getTime() <= boundary) continue;
    const key = item.issue_id ?? item.id;
    const group = groups.get(key);
    if (group) group.push(item);
    else groups.set(key, [item]);
  }

  const result: ActivityGroup[] = [];
  for (const [key, group] of groups) {
    group.sort(
      (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );
    const latest = group[0]!;
    result.push({
      key,
      issueId: latest.issue_id,
      items: group,
      latest,
      unreadCount: group.filter((item) => !item.read).length,
    });
  }
  return result.sort(
    (a, b) =>
      new Date(b.latest.created_at).getTime() - new Date(a.latest.created_at).getTime(),
  );
}
