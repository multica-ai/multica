import { describe, expect, it } from "vitest";
import {
  noteInboxAutoArchive,
  noteInboxBarAction,
  takeInboxAutoArchive,
  takeInboxAutoArchiveReason,
  takeInboxBarAction,
} from "./auto-archive";

describe("inbox auto-archive records", () => {
  it.each([
    {
      label: "issue_status_terminal",
      payload: { issue_id: "issue-completed", reason: "issue_status_terminal" },
      expected: "issue_status_terminal",
    },
    {
      label: "actor_acted",
      payload: { issue_id: "issue-acted", reason: "actor_acted" },
      expected: "actor_acted",
    },
    {
      label: "actor_acted + trigger=agent_triggered",
      payload: { issue_id: "issue-agent-wire", reason: "actor_acted", trigger: "agent_triggered" },
      expected: "agent_triggered",
    },
    {
      label: "legacy intermediate reason=agent_triggered",
      payload: { issue_id: "issue-agent-legacy", reason: "agent_triggered" },
      expected: "agent_triggered",
    },
    {
      label: "actor_acted with unknown future trigger falls back to actor_acted",
      payload: { issue_id: "issue-future-trigger", reason: "actor_acted", trigger: "future_trigger" },
      expected: "actor_acted",
    },
    {
      label: "issue_status_terminal wins over trigger",
      payload: {
        issue_id: "issue-completed-wins",
        reason: "issue_status_terminal",
        trigger: "agent_triggered",
      },
      expected: "issue_status_terminal",
    },
  ] as const)(
    "records an archive the user caused: $label",
    ({ payload, expected }) => {
      noteInboxAutoArchive(payload, 1_000);
      expect(takeInboxAutoArchiveReason(payload.issue_id, 2_000)).toBe(expected);
      expect(takeInboxAutoArchiveReason(payload.issue_id, 2_000)).toBeNull();

      noteInboxAutoArchive({ ...payload, issue_id: `bool-${payload.issue_id}` }, 1_000);
      expect(takeInboxAutoArchive(`bool-${payload.issue_id}`, 2_000)).toBe(true);
      expect(takeInboxAutoArchive(`bool-${payload.issue_id}`, 2_000)).toBe(false);
    },
  );

  it("ignores batches without a reason the user caused", () => {
    noteInboxAutoArchive({ issue_id: "issue-a" }, 1_000);
    noteInboxAutoArchive({ issue_id: "issue-b", reason: "retention" }, 1_000);
    noteInboxAutoArchive({ issue_id: "issue-b2", reason: "retention", trigger: "agent_triggered" }, 1_000);
    noteInboxAutoArchive({ reason: "actor_acted" }, 1_000);
    expect(takeInboxAutoArchive("issue-a", 1_000)).toBe(false);
    expect(takeInboxAutoArchive("issue-b", 1_000)).toBe(false);
    expect(takeInboxAutoArchive("issue-b2", 1_000)).toBe(false);
  });

  it("forgets a record once it is stale", () => {
    noteInboxAutoArchive({ issue_id: "issue-c", reason: "actor_acted" }, 1_000);
    expect(takeInboxAutoArchive("issue-c", 1_000 + 30_001)).toBe(false);
  });

  it("remains backward-compatible with v0.12.0 client parsing on agent-triggered events", () => {
    // Mirrors v0.12.0 packages/core/inbox/auto-archive.ts, which only recognized
    // `issue_status_terminal` and `actor_acted` before `useMoveOnAfterAutoArchive`
    // would auto-advance (`after_auto_archive = "next"`) and show the undo toast.
    const v0120Reasons = new Set(["issue_status_terminal", "actor_acted"]);
    const v0120Accepts = (payload: { issue_id?: string; reason?: string; trigger?: string }) =>
      Boolean(payload.issue_id && payload.reason && v0120Reasons.has(payload.reason));

    const wirePayload = {
      recipient_id: "member-1",
      count: 1,
      issue_id: "issue-agent-compat",
      reason: "actor_acted",
      trigger: "agent_triggered",
    };

    expect(v0120Accepts(wirePayload)).toBe(true);
    expect(v0120Accepts({ issue_id: "issue-broken", reason: "agent_triggered" })).toBe(false);

    noteInboxAutoArchive(wirePayload, 1_000);
    expect(takeInboxAutoArchiveReason("issue-agent-compat", 2_000)).toBe("agent_triggered");
  });
});

describe("inbox bar actions", () => {
  it("keeps the latest action on an issue until it is taken", () => {
    noteInboxBarAction("issue-a", { type: "status", status: "in_progress" }, 1_000);
    noteInboxBarAction("issue-a", { type: "quick_action", name: "Code Review" }, 1_500);
    expect(takeInboxBarAction("issue-a", 2_000)).toEqual({ type: "quick_action", name: "Code Review" });
    expect(takeInboxBarAction("issue-a", 2_000)).toBeNull();
  });

  it("forgets an action once it is stale", () => {
    noteInboxBarAction("issue-b", { type: "assignee", assignee: null }, 1_000);
    expect(takeInboxBarAction("issue-b", 1_000 + 30_001)).toBeNull();
  });
});
