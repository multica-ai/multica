// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue, IssuePriority, IssueStatusCategory } from "@multica/core/types";
import { DONE_LIMIT, groupAgentWork } from "./agent-work";

const CATEGORIES: Record<string, IssueStatusCategory> = {
  backlog: "unstarted",
  todo: "unstarted",
  in_progress: "started",
  in_review: "started",
  done: "done",
  cancelled: "closed",
  review_queue: "started",
};
const categoryOf = (status: string) => CATEGORIES[status];

function issue(
  id: string,
  status: string,
  options: { assignee?: string; priority?: IssuePriority; created?: string; updated?: string; title?: string } = {},
): Issue {
  return {
    id,
    identifier: `MUL-${id}`,
    title: options.title ?? `Task ${id}`,
    status,
    priority: options.priority ?? "none",
    assignee_type: options.assignee ? "agent" : null,
    assignee_id: options.assignee ?? null,
    created_at: options.created ?? "2026-01-01T00:00:00Z",
    updated_at: options.updated ?? "2026-01-01T00:00:00Z",
  } as Issue;
}

const ids = (list: Issue[]) => list.map((i) => i.id);

describe("groupAgentWork", () => {
  it("sorts the agent's issues by what its status category says", () => {
    const groups = groupAgentWork(
      [
        issue("a", "in_progress", { assignee: "me" }),
        issue("b", "in_review", { assignee: "me" }),
        issue("c", "todo", { assignee: "me" }),
        issue("d", "backlog", { assignee: "me" }),
        issue("e", "done", { assignee: "me" }),
        issue("f", "cancelled", { assignee: "me" }),
      ],
      "me",
      categoryOf,
    );

    expect(ids(groups.active)).toEqual(["a", "b"]);
    expect(ids(groups.next)).toEqual(["c", "d"]);
    expect(ids(groups.done).sort()).toEqual(["e", "f"]);
    expect(groups.assignable).toEqual([]);
  });

  it("files a custom status where its category says", () => {
    const groups = groupAgentWork([issue("a", "review_queue", { assignee: "me" })], "me", categoryOf);

    expect(ids(groups.active)).toEqual(["a"]);
  });

  it("offers work nobody has started, unassigned first, and never started or finished work of others", () => {
    const groups = groupAgentWork(
      [
        issue("held", "todo", { assignee: "other" }),
        issue("free", "backlog"),
        issue("busy", "in_progress", { assignee: "other" }),
        issue("closed", "done", { assignee: "other" }),
        issue("free-done", "done"),
      ],
      "me",
      categoryOf,
    );

    expect(ids(groups.assignable)).toEqual(["free", "held"]);
    expect(groups.active).toEqual([]);
    expect(groups.done).toEqual([]);
  });

  it("offers an issue whose status the catalog does not know yet", () => {
    const groups = groupAgentWork([issue("new", "brand_new")], "me", categoryOf);

    expect(ids(groups.assignable)).toEqual(["new"]);
  });

  it("puts the most urgent issue first, then the oldest", () => {
    const groups = groupAgentWork(
      [
        issue("low", "todo", { assignee: "me", priority: "low" }),
        issue("new-high", "todo", { assignee: "me", priority: "high", created: "2026-02-01T00:00:00Z" }),
        issue("old-high", "todo", { assignee: "me", priority: "high", created: "2026-01-01T00:00:00Z" }),
        issue("urgent", "todo", { assignee: "me", priority: "urgent" }),
        issue("none", "todo", { assignee: "me" }),
      ],
      "me",
      categoryOf,
    );

    expect(ids(groups.next)).toEqual(["urgent", "old-high", "new-high", "low", "none"]);
  });

  it("lists finished issues most recently touched first", () => {
    const groups = groupAgentWork(
      [
        issue("old", "done", { assignee: "me", updated: "2026-01-01T00:00:00Z" }),
        issue("fresh", "done", { assignee: "me", updated: "2026-03-01T00:00:00Z" }),
      ],
      "me",
      categoryOf,
    );

    expect(ids(groups.done)).toEqual(["fresh", "old"]);
  });

  it("filters every section by title or identifier", () => {
    const groups = groupAgentWork(
      [
        issue("1", "todo", { assignee: "me", title: "Fix the login bug" }),
        issue("2", "todo", { assignee: "me", title: "Write docs" }),
        issue("3", "backlog", { title: "Login page copy" }),
      ],
      "me",
      categoryOf,
      "  LOGIN ",
    );

    expect(ids(groups.next)).toEqual(["1"]);
    expect(ids(groups.assignable)).toEqual(["3"]);
    expect(ids(groupAgentWork([issue("7", "todo", { assignee: "me" })], "me", categoryOf, "mul-7").next)).toEqual(["7"]);
  });

  it("exposes how many finished issues the list shows", () => {
    expect(DONE_LIMIT).toBeGreaterThan(0);
  });
});
