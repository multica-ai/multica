import type { TimelineGroup } from "./grouping";

export interface PartitionedSessionGroups {
  // Comment threads — rendered prominently, in order.
  commentGroups: TimelineGroup[];
  // Activity blocks — folded into one quiet "Activity" section, in order.
  activityGroups: TimelineGroup[];
}

// Split a session's timeline into comment threads and activity blocks so the
// host can render comments prominently and tuck activity into a single fold
// (FIR-1769 P2). Order within each subset is preserved; nothing is dropped.
export function partitionSessionGroups(groups: TimelineGroup[]): PartitionedSessionGroups {
  const commentGroups: TimelineGroup[] = [];
  const activityGroups: TimelineGroup[] = [];
  for (const g of groups) {
    if (g.type === "comment") commentGroups.push(g);
    else activityGroups.push(g);
  }
  return { commentGroups, activityGroups };
}

// Total number of activity entries across the given groups — drives the
// "Activity (N)" fold label.
export function countActivityEntries(groups: TimelineGroup[]): number {
  let n = 0;
  for (const g of groups) n += g.entries.length;
  return n;
}
