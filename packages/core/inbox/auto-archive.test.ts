import { describe, expect, it } from "vitest";
import { noteInboxAutoArchive, noteInboxBarAction, takeInboxAutoArchive, takeInboxBarAction } from "./auto-archive";

describe("inbox auto-archive records", () => {
  it.each(["issue_status_terminal", "actor_acted"])("records an archive the user caused: %s", (reason) => {
    noteInboxAutoArchive({ issue_id: `issue-${reason}`, reason }, 1_000);
    expect(takeInboxAutoArchive(`issue-${reason}`, 2_000)).toBe(true);
    expect(takeInboxAutoArchive(`issue-${reason}`, 2_000)).toBe(false);
  });

  it("ignores batches without a reason the user caused", () => {
    noteInboxAutoArchive({ issue_id: "issue-a" }, 1_000);
    noteInboxAutoArchive({ issue_id: "issue-b", reason: "retention" }, 1_000);
    noteInboxAutoArchive({ reason: "actor_acted" }, 1_000);
    expect(takeInboxAutoArchive("issue-a", 1_000)).toBe(false);
    expect(takeInboxAutoArchive("issue-b", 1_000)).toBe(false);
  });

  it("forgets a record once it is stale", () => {
    noteInboxAutoArchive({ issue_id: "issue-c", reason: "actor_acted" }, 1_000);
    expect(takeInboxAutoArchive("issue-c", 1_000 + 30_001)).toBe(false);
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
