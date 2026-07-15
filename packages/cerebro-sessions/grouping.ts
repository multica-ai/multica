import type { TimelineEntry } from "@multica/core/types";
import type { Session } from "./types";
import { normalizeSessionMode } from "./modes";

export type TimelineGroup = { type: "activities" | "comment"; entries: TimelineEntry[] };

export interface SessionGroup {
  session: Session;
  // FIR-1874: open/closed is the thread root's resolved_at, surfaced here so the
  // host renders state without re-reading the entry.
  resolved: boolean;
  groups: TimelineGroup[];
}

// The implicit session shown when an issue has no comment threads yet: one
// "Session 1" containing whatever timeline there is (e.g. activity). No row.
export function defaultSession(issueId: string, name = "Session 1"): Session {
  return {
    id: "default",
    issue_id: issueId,
    root_comment_id: null,
    position: 0,
    name,
    mode: "build",
    phase: null,
    handoff: null,
    created_at: "",
    updated_at: "",
  };
}

// FIR-1874: a session IS a comment thread. Each "comment" timeline group (a
// thread: root + replies) becomes one session box, identified by its thread
// root's id. Activity blocks attach to the thread that precedes them (the thread
// whose run they are); activity before the first thread holds until the first
// thread appears. Session metadata (name/handoff) comes from the optional
// cerebro_session row keyed to the same root_comment_id; otherwise the name
// defaults to "Session N". Open/closed is the thread root's resolved_at — no
// status, no time-window markers.
export function groupTimelineByThread(
  issueId: string,
  sessionRows: Session[] | undefined,
  groups: TimelineGroup[],
): SessionGroup[] {
  const rowByRoot = new Map<string, Session>();
  for (const s of sessionRows ?? []) {
    if (s.root_comment_id) rowByRoot.set(s.root_comment_id, s);
  }

  const result: SessionGroup[] = [];
  let leading: TimelineGroup[] = []; // activity before the first thread
  let n = 0;

  for (const group of groups) {
    if (group.type === "comment") {
      const root = group.entries[0];
      if (!root) continue;
      n += 1;
      const row = rowByRoot.get(root.id);
      const name = row && row.name ? row.name : `Session ${n}`;
      const session: Session = {
        id: root.id,
        issue_id: issueId,
        root_comment_id: root.id,
        position: row?.position ?? n,
        name,
        mode: normalizeSessionMode(row?.mode),
        phase: row?.phase ?? null,
        handoff: row?.handoff ?? null,
        created_at: row?.created_at ?? root.created_at,
        updated_at: row?.updated_at ?? root.created_at,
      };
      result.push({
        session,
        resolved: !!root.resolved_at,
        groups: [...leading, group],
      });
      leading = [];
    } else if (result.length > 0) {
      result[result.length - 1]!.groups.push(group);
    } else {
      leading.push(group);
    }
  }

  // No threads at all: one implicit Session 1 holding whatever timeline exists.
  if (result.length === 0 && leading.length > 0) {
    result.push({ session: defaultSession(issueId), resolved: false, groups: leading });
  }
  return result;
}

// The id of the ACTIVE session = the latest OPEN (unresolved) thread, else the
// latest thread, else "default". Drives which session box is expanded.
export function activeSessionId(sessionGroups: SessionGroup[]): string {
  if (sessionGroups.length === 0) return "default";
  for (let i = sessionGroups.length - 1; i >= 0; i--) {
    if (!sessionGroups[i]!.resolved) return sessionGroups[i]!.session.id;
  }
  return sessionGroups[sessionGroups.length - 1]!.session.id;
}

// Resolve which session (thread) owns a single point in time, from the optional
// session rows. Best-effort: rows are sparse (only threads with a name/handoff
// have one), so this returns null when no row precedes the time. Used by the
// run-log label, which renders nothing when null.
export function sessionForTime(
  sessions: Session[] | undefined,
  isoTime: string,
): Session | null {
  const list = (sessions ?? []).slice().sort((a, b) => {
    const ta = new Date(a.created_at).getTime();
    const tb = new Date(b.created_at).getTime();
    if (ta !== tb) return ta - tb;
    return a.position - b.position;
  });
  if (list.length === 0) return null;

  const t = new Date(isoTime).getTime();
  const starts = list.map((s) => new Date(s.created_at).getTime());
  let idx = -1;
  for (let i = 0; i < starts.length; i++) {
    if ((starts[i] ?? 0) <= t) idx = i;
    else break;
  }
  return idx >= 0 ? list[idx]! : null;
}

// Resolve which session (thread) a comment belongs to: its thread root id. A
// reply is walked up to its root. Returns null when the comment isn't found.
export function sessionIdForComment(
  timeline: TimelineEntry[],
  _sessions: Session[] | undefined,
  commentId: string,
): string | null {
  const byId = new Map(timeline.map((e) => [e.id, e] as const));
  let entry = byId.get(commentId);
  if (!entry) return null;
  while (entry.parent_id) {
    const parent = byId.get(entry.parent_id);
    if (!parent) break;
    entry = parent;
  }
  return entry.id;
}
