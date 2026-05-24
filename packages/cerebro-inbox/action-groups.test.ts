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
  it("puts a fresh unread action-required item in Act now", () => {
    expect(
      classifyInboxAction(notif({ type: "mentioned", severity: "action_required", read: false }), ctx()),
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

  it("keeps a fresh action-required item in Act now even while the agent runs", () => {
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

  it("treats your own new comment as Calm (you had the last word)", () => {
    expect(
      classifyInboxAction(
        notif({ type: "new_comment", severity: "info", actor_type: "member", actor_id: USER, read: true }),
        ctx(),
      ),
    ).toBe("calm");
  });

  it("puts settled info items (e.g. task completed) in Calm", () => {
    expect(
      classifyInboxAction(notif({ type: "task_completed", severity: "info", read: true }), ctx()),
    ).toBe("calm");
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

  it("puts an unread chat reply in Waiting", () => {
    expect(
      classifyInboxAction({ kind: "chat", session: { id: "c-1", has_unread: true } }, ctx()),
    ).toBe("waiting");
  });

  it("puts a read chat in Calm", () => {
    expect(
      classifyInboxAction({ kind: "chat", session: { id: "c-1", has_unread: false } }, ctx()),
    ).toBe("calm");
  });

  it("puts a mentioned channel in Act now", () => {
    expect(
      classifyInboxAction(
        { kind: "channel", channel: { id: "ch-1", unread_count: 3 } },
        ctx({ mentionedChannels: new Set(["ch-1"]) }),
      ),
    ).toBe("act_now");
  });

  it("puts an unread (non-mentioned) channel in Waiting", () => {
    expect(
      classifyInboxAction({ kind: "channel", channel: { id: "ch-1", unread_count: 2 } }, ctx()),
    ).toBe("waiting");
  });
});

describe("ordering + bucketize adapter", () => {
  it("orders categories Act now → Watching → Waiting → Calm", () => {
    expect(INBOX_ACTION_ORDER).toEqual(["act_now", "watching", "waiting", "calm"]);
    expect(inboxActionOrderIndex("act_now")).toBeLessThan(inboxActionOrderIndex("calm"));
  });

  it("returns key, localized label, and sort order from bucketizeInboxAction", () => {
    const labels = { act_now: "Act now", watching: "Watching", waiting: "Waiting", calm: "Calm" };
    const bucket = bucketizeInboxAction(
      notif({ type: "mentioned", severity: "action_required", read: false }),
      ctx(),
      labels,
    );
    expect(bucket).toEqual({ key: "act_now", label: "Act now", isFallback: false, order: 0 });
  });
});
