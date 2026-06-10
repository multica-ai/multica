import { describe, expect, it } from "vitest";
import type { InboxItem } from "@multica/core/types";
import {
  INBOX_ACTION_ORDER,
  bucketizeInboxAction,
  classifyInboxAction,
  inboxActionOrderIndex,
  type InboxActionContext,
} from "./action-groups";

const USER = "user-1";

function ctx(overrides: Partial<InboxActionContext> = {}): InboxActionContext {
  return {
    userId: USER,
    issueRunStates: new Map(),
    subIssueRunStates: new Map(),
    chatRunStates: new Map(),
    mentionedChannels: new Set(),
    ...overrides,
  };
}

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: USER,
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    route: "inbox",
    issue_id: "issue-1",
    project_id: null,
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    muted_until: null,
    created_at: "2026-05-24T12:00:00Z",
    details: null,
    ...overrides,
  };
}

const notif = (overrides: Partial<InboxItem>) =>
  ({ kind: "notif", item: item(overrides) }) as const;

describe("classifyInboxAction — notifications", () => {
  it("puts every unread notification in Unread", () => {
    expect(
      classifyInboxAction(notif({ type: "new_comment", severity: "info", read: false }), ctx()),
    ).toBe("act_now");
  });

  it("demotes a read action-required item to Waiting (seen, not closed)", () => {
    expect(
      classifyInboxAction(notif({ type: "mentioned", severity: "action_required", read: true }), ctx()),
    ).toBe("waiting");
  });

  it("puts an item under Watching when its issue has an in-flight agent run", () => {
    expect(
      classifyInboxAction(
        notif({ severity: "attention", issue_id: "issue-9", read: true }),
        ctx({ issueRunStates: new Map([["issue-9", "active"]]) }),
      ),
    ).toBe("watching");
  });

  it("puts a parent under Watching when a sub-issue has an in-flight run (FIR-2326)", () => {
    expect(
      classifyInboxAction(
        notif({ issue_id: "parent-1", read: true, issue_status: "in_progress" }),
        ctx({ subIssueRunStates: new Map([["parent-1", "active"]]) }),
      ),
    ).toBe("watching");
  });

  it("keeps a fresh action-required item in Unread even while the agent runs", () => {
    expect(
      classifyInboxAction(
        notif({ severity: "action_required", issue_id: "issue-9", read: false }),
        ctx({ issueRunStates: new Map([["issue-9", "active"]]) }),
      ),
    ).toBe("act_now");
  });

  it("treats someone else's new comment as Waiting (last comment not yours)", () => {
    expect(
      classifyInboxAction(
        notif({ type: "new_comment", severity: "info", actor_type: "agent", actor_id: "agent-2", read: true }),
        ctx(),
      ),
    ).toBe("waiting");
  });

  it("keeps a read open issue in Waiting, even when you had the last word", () => {
    expect(
      classifyInboxAction(
        notif({
          type: "new_comment",
          severity: "info",
          actor_type: "member",
          actor_id: USER,
          read: true,
          issue_status: "in_progress",
        }),
        ctx(),
      ),
    ).toBe("waiting");
  });

  it("puts terminal issues in Done", () => {
    expect(
      classifyInboxAction(notif({ type: "new_comment", severity: "info", read: true, issue_status: "done" }), ctx()),
    ).toBe("calm");
  });

  // FIR-2445 — reminders and date-arrival notifications get their own bucket.
  it("puts a reminder in Reminders even when it is unread", () => {
    expect(
      classifyInboxAction(notif({ type: "reminder", read: false }), ctx()),
    ).toBe("reminders");
  });

  it("puts a due-date reminder in Reminders", () => {
    expect(
      classifyInboxAction(notif({ type: "due_date_reminder", read: true }), ctx()),
    ).toBe("reminders");
  });

  it("puts a start-date reminder in Reminders", () => {
    expect(
      classifyInboxAction(notif({ type: "start_date_reminder", read: false }), ctx()),
    ).toBe("reminders");
  });
});

describe("classifyInboxAction — chats and channels", () => {
  it("puts a running chat under Watching", () => {
    expect(
      classifyInboxAction(
        { kind: "chat", session: { id: "c-1", has_unread: false } },
        ctx({ chatRunStates: new Map([["c-1", "active"]]) }),
      ),
    ).toBe("watching");
  });

  it("puts an unread chat reply in Unread", () => {
    expect(
      classifyInboxAction({ kind: "chat", session: { id: "c-1", has_unread: true } }, ctx()),
    ).toBe("act_now");
  });

  it("puts a read chat in Calm", () => {
    expect(
      classifyInboxAction({ kind: "chat", session: { id: "c-1", has_unread: false } }, ctx()),
    ).toBe("calm");
  });

  it("puts a mentioned channel in Unread", () => {
    expect(
      classifyInboxAction(
        { kind: "channel", channel: { id: "ch-1", unread_count: 3 } },
        ctx({ mentionedChannels: new Set(["ch-1"]) }),
      ),
    ).toBe("act_now");
  });

  it("puts an unread channel in Unread", () => {
    expect(
      classifyInboxAction({ kind: "channel", channel: { id: "ch-1", unread_count: 2 } }, ctx()),
    ).toBe("act_now");
  });

  it("puts a read channel where I sent the last message in Pending", () => {
    expect(
      classifyInboxAction(
        { kind: "channel", channel: { id: "ch-1", unread_count: 0, last_message: { author_type: "member", author_id: USER } } },
        ctx(),
      ),
    ).toBe("pending");
  });

  it("puts a read channel where an agent sent the last message in Pending", () => {
    expect(
      classifyInboxAction(
        { kind: "channel", channel: { id: "ch-1", unread_count: 0, last_message: { author_type: "agent", author_id: "agent-99" } } },
        ctx(),
      ),
    ).toBe("pending");
  });

  it("puts a read channel where another member sent the last message in Done", () => {
    expect(
      classifyInboxAction(
        { kind: "channel", channel: { id: "ch-1", unread_count: 0, last_message: { author_type: "member", author_id: "other-user" } } },
        ctx(),
      ),
    ).toBe("calm");
  });

  it("puts a read channel with no last_message in Done", () => {
    expect(
      classifyInboxAction(
        { kind: "channel", channel: { id: "ch-1", unread_count: 0, last_message: null } },
        ctx(),
      ),
    ).toBe("calm");
  });
});

describe("ordering + bucketize adapter", () => {
  it("orders categories Unread → Reminders → Running → Pending → Done → Waiting", () => {
    expect(INBOX_ACTION_ORDER).toEqual(["act_now", "reminders", "watching", "pending", "calm", "waiting"]);
    expect(inboxActionOrderIndex("act_now")).toBeLessThan(inboxActionOrderIndex("reminders"));
    expect(inboxActionOrderIndex("reminders")).toBeLessThan(inboxActionOrderIndex("watching"));
    expect(inboxActionOrderIndex("watching")).toBeLessThan(inboxActionOrderIndex("pending"));
    expect(inboxActionOrderIndex("pending")).toBeLessThan(inboxActionOrderIndex("calm"));
    expect(inboxActionOrderIndex("calm")).toBeLessThan(inboxActionOrderIndex("waiting"));
  });

  it("returns key, localized label, and sort order from bucketizeInboxAction", () => {
    const labels = { act_now: "Unread", reminders: "Reminders", watching: "Running", pending: "Pending", waiting: "Waiting", calm: "Done" };
    const bucket = bucketizeInboxAction(
      notif({ type: "mentioned", severity: "action_required", read: false }),
      ctx(),
      labels,
    );
    expect(bucket).toEqual({ key: "act_now", label: "Unread", isFallback: false, order: 0 });
  });

  it("places Reminders right below Unread", () => {
    const labels = { act_now: "Unread", reminders: "Reminders", watching: "Running", pending: "Pending", waiting: "Waiting", calm: "Done" };
    const bucket = bucketizeInboxAction(
      notif({ type: "reminder", read: false }),
      ctx(),
      labels,
    );
    expect(bucket).toEqual({ key: "reminders", label: "Reminders", isFallback: false, order: 1 });
  });
});
