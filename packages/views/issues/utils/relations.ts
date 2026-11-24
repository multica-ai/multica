import type { Issue, IssueBlocker } from "@multica/core/types";

export interface IssueRelations {
  /** Unfinished issues this one waits for. */
  blockedBy: IssueBlocker[];
  /** Unfinished issues waiting for this one. */
  blocking: IssueBlocker[];
  /** Issues marked as duplicates of this one. */
  duplicates: Issue[];
  /** Unfinished blockers the reader may not see, counted but not listed. */
  restricted: number;
  /** Blockers that ended, and all of them, for "1/3 ended". */
  finished: number;
  total: number;
  empty: boolean;
}

/**
 * What the sidebar's Relations section lists (MUL-10050). Finished issues
 * drop out of both blocker groups: they no longer hold anything up, and the
 * ended count keeps the progress. The section renders nothing when empty, and
 * the sidebar's add row offers relations instead.
 */
export function issueRelations(issue: Issue, duplicates: Issue[] = []): IssueRelations {
  const all = issue.blocked_by ?? [];
  const blockedBy = all.filter((b) => !b.closed);
  const blocking = (issue.blocking ?? []).filter((b) => !b.closed);
  const restricted = Math.max(0, (issue.blocked_count ?? 0) - blockedBy.length);
  return {
    blockedBy,
    blocking,
    duplicates,
    restricted,
    finished: all.length - blockedBy.length,
    total: all.length + restricted,
    empty: !blockedBy.length && !restricted && !blocking.length && !duplicates.length,
  };
}
