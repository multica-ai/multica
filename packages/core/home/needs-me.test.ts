// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { InboxItem, Issue } from "../types";
import { groupActivityByIssue } from "./activity";
import { resolveHomeSince } from "./last-visit-store";
import { isReplyPending } from "./replies-store";
import {
  compareNeedsMe,
  deriveNeedsMe,
  dueBucket,
  needsMeReasons,
  NEEDS_ME_INBOX_WINDOW_DAYS,
} from "./needs-me";

const NOW = Date.parse("2026-09-21T12:00:00Z");
const TODAY = "2026-09-21";

function issue(overrides: Partial<Issue>): Issue {
  return {
    id: "i1",
    workspace_id: "ws",
    number: 1,
    identifier: "MUL-1",
    title: "Issue",
    description: null,
    status: "in_review",
    priority: "none",
    assignee_type: "agent",
    assignee_id: "agent-1",
    creator_type: "member",
    creator_id: "me",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-21T10:00:00Z",
    ...overrides,
  };
}

function inbox(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "n1",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "me",
    actor_type: "member",
    actor_id: "u2",
    type: "mentioned",
    severity: "info",
    issue_id: "i9",
    title: "Mentioned issue",
    body: null,
    issue_status: "in_progress",
    issue_priority: "none",
    read: false,
    archived: false,
    created_at: "2026-09-21T09:00:00Z",
    details: null,
    ...overrides,
  };
}

describe("deriveNeedsMe", () => {
  it("turns in_review and blocked issues into entries waiting on their assignee", () => {
    const items = deriveNeedsMe({
      statusIssues: [
        issue({ id: "a", status: "in_review" }),
        issue({ id: "b", status: "blocked", assignee_id: "agent-2" }),
      ],
      inboxItems: [],
      now: NOW,
    });
    expect(items.map((i) => [i.kind, i.issueId, i.actor?.id])).toEqual([
      ["blocked", "b", "agent-2"],
      ["in_review", "a", "agent-1"],
    ]);
  });

  it("drops a cached row an optimistic patch already moved out of the status", () => {
    const items = deriveNeedsMe({
      statusIssues: [issue({ status: "done" })],
      inboxItems: [],
      now: NOW,
    });
    expect(items).toEqual([]);
  });

  it("keeps mentions and action_required items, and nothing else from the inbox", () => {
    const items = deriveNeedsMe({
      statusIssues: [],
      inboxItems: [
        inbox({ id: "m", issue_id: "i1", type: "mentioned" }),
        inbox({ id: "f", issue_id: "i2", type: "task_failed", severity: "action_required" }),
        inbox({ id: "c", issue_id: "i3", type: "new_comment", severity: "info" }),
        inbox({ id: "x", issue_id: "i4", archived: true }),
      ],
      now: NOW,
    });
    expect(items.map((i) => [i.kind, i.inboxIds])).toEqual([
      ["mentioned", ["m"]],
      ["action_required", ["f"]],
    ]);
  });

  it("folds inbox rows on an already-queued issue into that entry", () => {
    const items = deriveNeedsMe({
      statusIssues: [issue({ id: "i1", status: "blocked" })],
      inboxItems: [
        inbox({ id: "m1", issue_id: "i1", created_at: "2026-09-21T08:00:00Z" }),
        inbox({ id: "m2", issue_id: "i1", created_at: "2026-09-21T09:00:00Z" }),
      ],
      now: NOW,
    });
    expect(items).toHaveLength(1);
    expect(items[0]!.kind).toBe("blocked");
    expect(items[0]!.inboxIds).toEqual(["m1", "m2"]);
    expect(items[0]!.inbox?.id).toBe("m2");
  });

  it("lets a mention take over a generic action on the same issue", () => {
    const items = deriveNeedsMe({
      statusIssues: [],
      inboxItems: [
        inbox({ id: "a", issue_id: "i1", type: "issue_assigned", severity: "action_required", actor_id: "u3", created_at: "2026-09-21T07:00:00Z" }),
        inbox({ id: "m", issue_id: "i1", type: "mentioned", actor_id: "u2" }),
      ],
      now: NOW,
    });
    expect(items).toHaveLength(1);
    expect(items[0]!.kind).toBe("mentioned");
    expect(items[0]!.actor).toEqual({ type: "member", id: "u2" });
    expect(items[0]!.since).toBe("2026-09-21T07:00:00Z");
  });

  it("ignores inbox items outside the window and failures on finished issues", () => {
    const old = new Date(NOW - (NEEDS_ME_INBOX_WINDOW_DAYS + 1) * 86_400_000).toISOString();
    const items = deriveNeedsMe({
      statusIssues: [],
      inboxItems: [
        inbox({ id: "old", created_at: old }),
        inbox({ id: "done", type: "task_failed", severity: "action_required", issue_status: "done" }),
      ],
      now: NOW,
    });
    expect(items).toEqual([]);
  });

  it("keeps issue-less action items as their own entries", () => {
    const items = deriveNeedsMe({
      statusIssues: [],
      inboxItems: [
        inbox({ id: "q", issue_id: null, type: "quick_create_failed", severity: "action_required", issue_status: null }),
      ],
      now: NOW,
    });
    expect(items.map((i) => i.key)).toEqual(["inbox:q"]);
  });
});

describe("compareNeedsMe", () => {
  const base = deriveNeedsMe({ statusIssues: [issue({})], inboxItems: [], now: NOW })[0]!;

  it("ranks priority before due date, due date before blocking, blocking before age", () => {
    const urgent = { ...base, key: "u", priority: "urgent" as const };
    const dueToday = { ...base, key: "d", dueDate: TODAY };
    const blocked = { ...base, key: "b", kind: "blocked" as const };
    const older = { ...base, key: "o", since: "2026-09-01T00:00:00Z" };
    const sorted = [older, blocked, dueToday, urgent].sort((a, b) => compareNeedsMe(a, b, TODAY));
    expect(sorted.map((i) => i.key)).toEqual(["u", "d", "b", "o"]);
  });
});

describe("dueBucket", () => {
  it("buckets overdue, today, tomorrow, later and none", () => {
    expect(dueBucket("2026-09-20", TODAY)).toBe(0);
    expect(dueBucket(TODAY, TODAY)).toBe(1);
    expect(dueBucket("2026-09-22", TODAY)).toBe(2);
    expect(dueBucket("2026-09-30", TODAY)).toBe(3);
    expect(dueBucket(null, TODAY)).toBe(4);
  });
});

describe("needsMeReasons", () => {
  it("states priority, due date and waiting time in ranking order", () => {
    const [item] = deriveNeedsMe({
      statusIssues: [issue({ priority: "high", due_date: TODAY, updated_at: "2026-09-21T10:00:00Z" })],
      inboxItems: [],
      now: NOW,
    });
    expect(needsMeReasons(item!, NOW, TODAY)).toEqual([
      { kind: "priority", priority: "high" },
      { kind: "due_today" },
      { kind: "waiting", ms: 2 * 60 * 60 * 1000 },
    ]);
  });
});

describe("groupActivityByIssue", () => {
  it("groups activity after the boundary per issue, newest group first", () => {
    const groups = groupActivityByIssue(
      [
        inbox({ id: "a1", issue_id: "A", created_at: "2026-09-21T08:00:00Z", read: true }),
        inbox({ id: "b1", issue_id: "B", created_at: "2026-09-21T09:00:00Z" }),
        inbox({ id: "a2", issue_id: "A", created_at: "2026-09-21T10:00:00Z" }),
        inbox({ id: "old", issue_id: "C", created_at: "2026-09-20T00:00:00Z" }),
      ],
      "2026-09-21T00:00:00Z",
    );
    expect(groups.map((g) => [g.key, g.items.map((i) => i.id), g.unreadCount])).toEqual([
      ["A", ["a2", "a1"], 1],
      ["B", ["b1"], 1],
    ]);
  });
});

describe("resolveHomeSince", () => {
  const now = Date.parse("2026-09-21T12:00:00Z");
  const at = (minutesAgo: number) => new Date(now - minutesAgo * 60_000).toISOString();

  it("covers the last 24 hours on a first visit", () => {
    expect(resolveHomeSince(undefined, now)).toBe(at(24 * 60));
  });

  it("keeps the session's boundary across a short trip away", () => {
    expect(resolveHomeSince({ since: at(600), lastSeen: at(5) }, now)).toBe(at(600));
  });

  it("starts from the last departure after a long absence", () => {
    expect(resolveHomeSince({ since: at(600), lastSeen: at(120) }, now)).toBe(at(120));
  });
});

describe("isReplyPending", () => {
  const repliedAt = "2026-09-21T10:00:00Z";

  it("holds while the reply is the latest activity on the issue", () => {
    expect(isReplyPending(repliedAt, { last_activity_at: "2026-09-21T10:00:00.400Z" })).toBe(true);
  });

  it("lapses once anything happens after the reply", () => {
    expect(isReplyPending(repliedAt, { last_activity_at: "2026-09-21T10:05:00Z" })).toBe(false);
  });

  it("holds when the issue has no activity clock, and is off with no reply", () => {
    expect(isReplyPending(repliedAt, { last_activity_at: null })).toBe(true);
    expect(isReplyPending(undefined, { last_activity_at: null })).toBe(false);
  });
});
