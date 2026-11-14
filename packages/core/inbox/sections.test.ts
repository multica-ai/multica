// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentTask, InboxItem } from "../types";
import {
  buildInboxSections,
  inboxReasonKind,
  inboxWorkingRows,
  nextInboxWake,
  placeInboxGroup,
} from "./sections";

const NOW = new Date("2026-06-15T12:00:00Z").getTime();
const LATER = "2026-06-15T18:00:00Z";
const EARLIER = "2026-06-15T09:00:00Z";

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    section: "needs_you",
    ...overrides,
  };
}

function task(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-15T11:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-15T10:59:00Z",
    attribution: { source: "direct_human", precise: true, originator: { id: "me" } },
    ...overrides,
  } as AgentTask;
}

describe("placeInboxGroup", () => {
  it("keeps an issue under needs you after it has been read", () => {
    const mention = item({ type: "mentioned", read: true });
    expect(placeInboxGroup([mention], NOW)).toMatchObject({ section: "needs_you", reason: mention });
  });

  it("explains the row by the newest notification addressed to the user", () => {
    const update = item({ id: "update", section: "updates", created_at: "2026-06-15T11:00:00Z" });
    const mention = item({ id: "mention", type: "mentioned", created_at: "2026-06-15T10:00:00Z" });
    const assigned = item({ id: "assigned", type: "issue_assigned", created_at: "2026-06-15T09:00:00Z" });
    expect(placeInboxGroup([update, mention, assigned], NOW).reason).toBe(mention);
  });

  it("previews the newest notification that says something", () => {
    const handBack = item({ id: "done", type: "agent_completed", created_at: "2026-06-15T11:00:00Z" });
    const comment = item({ id: "comment", body: "PR is up", created_at: "2026-06-15T10:00:00Z" });
    const older = item({ id: "older", body: "Starting", created_at: "2026-06-15T09:00:00Z" });
    expect(placeInboxGroup([handBack, comment, older], NOW)).toMatchObject({
      reason: handBack,
      preview: comment,
    });
  });

  it("files an issue the user only follows under updates", () => {
    expect(placeInboxGroup([item({ section: "updates" })], NOW)).toMatchObject({
      section: "updates",
      reason: null,
    });
  });

  it("treats a row without a section as addressed to the user", () => {
    // A server that predates sections: its one list is what needs the user.
    const legacy = item({ section: undefined });
    expect(placeInboxGroup([legacy], NOW).section).toBe("needs_you");
  });

  it("puts a snoozed issue away until its time", () => {
    const snoozed = item({ snoozed_until: LATER });
    expect(placeInboxGroup([snoozed], NOW)).toMatchObject({
      section: "snoozed",
      reason: null,
      snoozedUntil: LATER,
    });
  });

  it("brings a snoozed issue back once its time has passed", () => {
    const woken = item({ snoozed_until: EARLIER });
    expect(placeInboxGroup([woken], NOW)).toMatchObject({
      section: "needs_you",
      reason: woken,
      wokeAt: new Date(EARLIER).getTime(),
    });
  });

  it("brings a snoozed issue back early when something new is addressed to the user", () => {
    const snoozed = item({ id: "old", snoozed_until: LATER });
    const question = item({ id: "new", type: "mentioned", created_at: "2026-06-15T11:30:00Z" });
    expect(placeInboxGroup([question, snoozed], NOW)).toMatchObject({
      section: "needs_you",
      reason: question,
    });
  });

  it("leaves a snoozed issue away when only activity arrives", () => {
    const snoozed = item({ id: "old", snoozed_until: LATER });
    const activity = item({ id: "new", section: "updates", created_at: "2026-06-15T11:30:00Z" });
    expect(placeInboxGroup([activity, snoozed], NOW).section).toBe("snoozed");
  });
});

describe("buildInboxSections", () => {
  it("lists each issue once, in the section its notifications put it", () => {
    const needs = item({ id: "a", issue_id: "issue-a", type: "mentioned" });
    const update = item({ id: "b", issue_id: "issue-b", section: "updates" });
    const snoozed = item({ id: "c", issue_id: "issue-c", snoozed_until: LATER });
    const all = [needs, update, snoozed];

    const sections = buildInboxSections(all, all, NOW);

    expect(sections.needsYou.map((row) => row.item.id)).toEqual(["a"]);
    expect(sections.updates.map((row) => row.item.id)).toEqual(["b"]);
    expect(sections.snoozed.map((row) => row.item.id)).toEqual(["c"]);
    expect(sections.snoozed[0]?.snoozedUntil).toBe(LATER);
    expect(sections.working).toEqual([]);
  });

  it("places a row by all of the issue's notifications, not the one a filter kept", () => {
    const mention = item({ id: "mention", type: "mentioned", created_at: "2026-06-15T09:00:00Z" });
    const update = item({ id: "update", section: "updates", created_at: "2026-06-15T10:00:00Z" });

    const sections = buildInboxSections([update], [mention, update], NOW);

    expect(sections.needsYou).toHaveLength(1);
    expect(sections.needsYou[0]).toMatchObject({ item: update, reason: mention });
    expect(sections.updates).toEqual([]);
  });

  it("puts an issue that just came back from a snooze first", () => {
    const fresh = item({ id: "fresh", issue_id: "issue-a", created_at: "2026-06-15T10:00:00Z" });
    const woken = item({
      id: "woken",
      issue_id: "issue-b",
      created_at: "2026-06-10T10:00:00Z",
      snoozed_until: "2026-06-15T11:00:00Z",
    });
    const all = [fresh, woken];

    expect(buildInboxSections(all, all, NOW).needsYou.map((row) => row.item.id)).toEqual([
      "woken",
      "fresh",
    ]);
  });

  it("orders snoozed issues by when they come back", () => {
    const tomorrow = item({ id: "tomorrow", issue_id: "issue-a", snoozed_until: "2026-06-16T09:00:00Z" });
    const tonight = item({ id: "tonight", issue_id: "issue-b", snoozed_until: LATER });
    const all = [tomorrow, tonight];

    expect(buildInboxSections(all, all, NOW).snoozed.map((row) => row.item.id)).toEqual([
      "tonight",
      "tomorrow",
    ]);
  });

  it("lists an issue the user's agent is working on under working, not updates", () => {
    const update = item({ id: "update", section: "updates" });
    const sections = buildInboxSections([update], [update], NOW, { working: { tasks: [task({})], userId: "me" } });

    expect(sections.working.map((row) => row.issueId)).toEqual(["issue-1"]);
    expect(sections.updates).toEqual([]);
  });

  it("keeps an issue that needs the user out of working", () => {
    const mention = item({ type: "mentioned" });
    const sections = buildInboxSections([mention], [mention], NOW, { working: { tasks: [task({})], userId: "me" } });

    expect(sections.needsYou).toHaveLength(1);
    expect(sections.working).toEqual([]);
  });

  it("keeps a snoozed issue out of working", () => {
    const snoozed = item({ section: "updates", snoozed_until: LATER });
    const sections = buildInboxSections([snoozed], [snoozed], NOW, { working: { tasks: [task({})], userId: "me" } });

    expect(sections.snoozed).toHaveLength(1);
    expect(sections.working).toEqual([]);
  });

  it("leaves a read update out and names it for the archive", () => {
    const unread = item({ id: "unread", issue_id: "issue-a", section: "updates" });
    const read = item({ id: "read", issue_id: "issue-b", section: "updates", read: true });
    const all = [unread, read];

    const sections = buildInboxSections(all, all, NOW);

    expect(sections.updates.map((row) => row.item.id)).toEqual(["unread"]);
    expect(sections.readUpdates).toEqual(["issue-b"]);
  });

  it("goes by the newest update, so a later unread one keeps the issue listed", () => {
    const older = item({ id: "older", section: "updates", read: true, created_at: "2026-06-15T09:00:00Z" });
    const newer = item({ id: "newer", section: "updates", created_at: "2026-06-15T10:00:00Z" });

    const sections = buildInboxSections([newer], [newer, older], NOW);

    expect(sections.updates.map((row) => row.item.id)).toEqual(["newer"]);
    expect(sections.readUpdates).toEqual([]);
  });

  it("keeps the update that is open until the user moves on", () => {
    const open = item({ id: "open", issue_id: "issue-a", section: "updates", read: true });
    const all = [open];

    expect(buildInboxSections(all, all, NOW, { openKey: "issue-a" })).toMatchObject({
      updates: [{ item: open }],
      readUpdates: [],
    });
    expect(buildInboxSections(all, all, NOW, { openKey: "issue-b" })).toMatchObject({
      updates: [],
      readUpdates: ["issue-a"],
    });
  });

  it("names a read update a filter left out, and never one that is read but needs the user", () => {
    const readUpdate = item({ id: "update", issue_id: "issue-a", section: "updates", read: true });
    const readMention = item({ id: "mention", issue_id: "issue-b", type: "mentioned", read: true });
    const snoozed = item({ id: "snoozed", issue_id: "issue-c", section: "updates", read: true, snoozed_until: LATER });

    const sections = buildInboxSections([readMention], [readUpdate, readMention, snoozed], NOW);

    expect(sections.needsYou.map((row) => row.item.id)).toEqual(["mention"]);
    expect(sections.readUpdates).toEqual(["issue-a"]);
  });
});

describe("inboxWorkingRows", () => {
  it("lists a run the user started, queued or running", () => {
    const rows = inboxWorkingRows(
      [
        task({ id: "running", issue_id: "issue-a" }),
        task({ id: "queued", issue_id: "issue-b", status: "queued", started_at: null }),
      ],
      "me",
    );
    expect(rows).toEqual([
      { issueId: "issue-a", taskId: "running", agentId: "agent-1", queued: false, since: "2026-06-15T11:00:00Z" },
      { issueId: "issue-b", taskId: "queued", agentId: "agent-1", queued: true, since: "2026-06-15T10:59:00Z" },
    ]);
  });

  it("leaves out runs someone or something else started", () => {
    const rows = inboxWorkingRows(
      [
        task({ id: "other", attribution: { source: "direct_human", precise: true, originator: { id: "someone" } } }),
        task({ id: "autopilot", attribution: { source: "rule_owner", precise: true, initiator: { id: "me" } } }),
        task({ id: "delegated", attribution: { source: "delegation", precise: true, originator: { id: "me" } } }),
        task({ id: "legacy", attribution: undefined }),
      ],
      "me",
    );
    expect(rows).toEqual([]);
  });

  it("leaves out finished runs and runs without an issue", () => {
    const rows = inboxWorkingRows(
      [
        task({ id: "done", status: "completed" }),
        task({ id: "failed", status: "failed" }),
        task({ id: "cancelled", status: "cancelled" }),
        task({ id: "chat", issue_id: "" }),
        task({ id: "parked", status: "deferred" }),
      ],
      "me",
    );
    expect(rows).toEqual([]);
  });

  it("shows one row per issue, the running run over a queued one", () => {
    const rows = inboxWorkingRows(
      [
        task({ id: "queued", status: "queued", started_at: null }),
        task({ id: "running" }),
      ],
      "me",
    );
    expect(rows.map((row) => row.taskId)).toEqual(["running"]);
  });

  it("lists nothing before the user is known", () => {
    expect(inboxWorkingRows([task({})], undefined)).toEqual([]);
  });
});

describe("nextInboxWake", () => {
  it("is the soonest snooze still running", () => {
    const items = [
      item({ id: "a", snoozed_until: "2026-06-16T09:00:00Z" }),
      item({ id: "b", snoozed_until: LATER }),
      item({ id: "c", snoozed_until: EARLIER }),
      item({ id: "d" }),
      item({ id: "e", archived: true, snoozed_until: "2026-06-15T12:30:00Z" }),
    ];
    expect(nextInboxWake(items, NOW)).toBe(new Date(LATER).getTime());
  });

  it("is null when nothing is snoozed", () => {
    expect(nextInboxWake([item({})], NOW)).toBeNull();
  });
});

describe("inboxReasonKind", () => {
  it("keeps the reason a notification names itself", () => {
    expect(inboxReasonKind(item({ type: "mentioned", issue_status: "in_review" }))).toBe("mention");
    expect(inboxReasonKind(item({ type: "issue_assigned" }))).toBe("assigned");
    expect(inboxReasonKind(item({ type: "task_failed", issue_status: "in_review" }))).toBe("failed");
    expect(inboxReasonKind(item({ type: "task_cancelled", issue_status: "in_progress" }))).toBe("cancelled");
    expect(inboxReasonKind(item({ type: "agent_blocked" }))).toBe("question");
    expect(inboxReasonKind(item({ type: "review_requested" }))).toBe("review");
  });

  it("reads a hand-over through the issue's status", () => {
    expect(inboxReasonKind(item({ type: "agent_completed", issue_status: "in_review" }))).toBe("review");
    expect(inboxReasonKind(item({ type: "agent_completed", issue_status: "blocked" }))).toBe("question");
    expect(inboxReasonKind(item({ type: "new_comment", issue_status: "in_review" }))).toBe("review");
    expect(inboxReasonKind(item({ type: "status_changed", issue_status: "blocked" }))).toBe("question");
  });

  it("falls back to what happened when the status asks for nothing", () => {
    expect(inboxReasonKind(item({ type: "agent_completed", issue_status: "in_progress" }))).toBe("finished");
    expect(inboxReasonKind(item({ type: "new_comment", issue_status: "todo" }))).toBe("comment");
  });

  it("leaves the rest to the notification's own label", () => {
    expect(inboxReasonKind(item({ type: "status_changed", issue_status: "todo" }))).toBeNull();
    expect(inboxReasonKind(item({ type: "app_action_requested" }))).toBeNull();
  });
});
