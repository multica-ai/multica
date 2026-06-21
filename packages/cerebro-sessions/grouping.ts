import type { TimelineEntry } from "@multica/core/types";
import type { Session } from "./types";

export type TimelineGroup = { type: "activities" | "comment"; entries: TimelineEntry[] };

export interface SessionGroup {
  session: Session;
  groups: TimelineGroup[];
}

// The implicit session shown when an issue has no session markers yet: one
// "Session 1" containing the whole timeline. No row is written for it.
export function defaultSession(issueId: string, name = "Session 1"): Session {
  return {
    id: "default",
    issue_id: issueId,
    position: 0,
    name,
    status: "in_progress",
    handoff: null,
    created_at: "",
    updated_at: "",
  };
}

// Model B membership: a session owns the contiguous slice of the timeline from
// its start marker up to the next session's start. Each timeline group is
// assigned to the latest session whose start time is at or before the group's
// first entry; groups earlier than the first marker fall into the earliest
// session, so nothing is ever orphaned. There is no per-comment field to set —
// which is exactly why a new comment always lands in the latest session.
export function groupTimelineBySession(
  issueId: string,
  sessions: Session[] | undefined,
  groups: TimelineGroup[],
): SessionGroup[] {
  const list = (sessions ?? []).slice().sort((a, b) => {
    const ta = new Date(a.created_at).getTime();
    const tb = new Date(b.created_at).getTime();
    if (ta !== tb) return ta - tb;
    return a.position - b.position;
  });

  // No real sessions yet: the whole timeline is one implicit Session 1.
  if (list.length === 0) {
    return [{ session: defaultSession(issueId), groups }];
  }

  const buckets = new Map<string, TimelineGroup[]>();
  for (const s of list) buckets.set(s.id, []);

  const starts = list.map((s) => new Date(s.created_at).getTime());
  for (const group of groups) {
    const first = group.entries[0];
    if (!first) continue;
    const t = new Date(first.created_at).getTime();
    let idx = 0;
    for (let i = 0; i < starts.length; i++) {
      if ((starts[i] ?? 0) <= t) idx = i;
      else break;
    }
    const target = list[idx];
    if (target) buckets.get(target.id)?.push(group);
  }

  // Keep every real session in chronological order, including an empty active
  // session so its header (and handoff) still shows before the first comment.
  return list.map((session) => ({ session, groups: buckets.get(session.id) ?? [] }));
}
