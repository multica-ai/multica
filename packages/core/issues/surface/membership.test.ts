import { describe, expect, it } from "vitest";
import type { Issue } from "../../types";
import {
  issueChangedDims,
  issueMatchesListFilter,
  listFilterDependsOn,
} from "./membership";

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Issue 1",
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: "member",
    assignee_id: "me",
    creator_type: "member",
    creator_id: "me",
    parent_issue_id: null,
    project_id: "p1",
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
  properties: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("issueMatchesListFilter", () => {
  it("judges assignee_id filters definitively", () => {
    const issue = makeIssue();
    expect(issueMatchesListFilter(issue, "assigned", { assignee_id: "me" })).toBe(true);
    expect(issueMatchesListFilter(issue, "assigned", { assignee_id: "bob" })).toBe(false);
  });

  it("degrades to unknown when the entity is missing the filtered field", () => {
    expect(
      issueMatchesListFilter({ title: "partial" }, "assigned", { assignee_id: "me" }),
    ).toBe("unknown");
  });

  it("judges assignee_types filters, treating unassigned as a definitive miss", () => {
    expect(
      issueMatchesListFilter(makeIssue(), "workspace:members", {
        assignee_types: ["member"],
      }),
    ).toBe(true);
    expect(
      issueMatchesListFilter(makeIssue({ assignee_type: "agent" }), "workspace:members", {
        assignee_types: ["member"],
      }),
    ).toBe(false);
    expect(
      issueMatchesListFilter(
        makeIssue({ assignee_type: null, assignee_id: null }),
        "workspace:agents",
        { assignee_types: ["agent", "squad"] },
      ),
    ).toBe(false);
  });

  it("judges project filters", () => {
    expect(
      issueMatchesListFilter(makeIssue(), "project:p1", { project_id: "p1" }),
    ).toBe(true);
    expect(
      issueMatchesListFilter(makeIssue({ project_id: "p2" }), "project:p1", {
        project_id: "p1",
      }),
    ).toBe(false);
    expect(
      issueMatchesListFilter(makeIssue({ project_id: null }), "project:p1", {
        project_id: "p1",
      }),
    ).toBe(false);
  });

  it("never decides involves_user_id — the ownership graph is server-side", () => {
    expect(
      issueMatchesListFilter(makeIssue(), "agents", { involves_user_id: "me" }),
    ).toBe("unknown");
  });

  it("never decides the my:all union scope", () => {
    expect(issueMatchesListFilter(makeIssue(), "all", {})).toBe("unknown");
  });

  it("ANDs across fields — a definitive miss beats an unknown", () => {
    expect(
      issueMatchesListFilter(
        makeIssue({ project_id: "p2" }),
        "scoped",
        { project_id: "p1", involves_user_id: "me" },
      ),
    ).toBe(false);
  });
});

describe("issueChangedDims", () => {
  it("treats written membership fields as changed when no base is known", () => {
    expect(issueChangedDims({ assignee_id: "bob", assignee_type: "member" })).toEqual({
      assignee: true,
      project: false,
      status: false,
    });
    expect(issueChangedDims({ project_id: null })).toEqual({
      assignee: false,
      project: true,
      status: false,
    });
  });

  it("sharpens against a base entity — writing the same value changes nothing", () => {
    const base = makeIssue();
    expect(issueChangedDims({ assignee_id: "me", assignee_type: "member" }, base)).toEqual({
      assignee: false,
      project: false,
      status: false,
    });
    expect(issueChangedDims({ status: "todo" }, base).status).toBe(false);
    expect(issueChangedDims({ status: "done" }, base).status).toBe(true);
    expect(issueChangedDims({ project_id: "p2" }, base).project).toBe(true);
  });

  it("treats a status_id change as a status change (MUL-4809)", () => {
    // The StatusPicker sends status_id, and two custom statuses in one Category
    // share the legacy `status` token. Keying only off `status` missed the move
    // and left the issue in its old column after settle.
    const base = makeIssue({ status: "in_progress", status_id: "cat-a" });
    expect(issueChangedDims({ status_id: "cat-b" }, base).status).toBe(true);
    // Same status_id written back is not a change.
    expect(issueChangedDims({ status_id: "cat-a" }, base).status).toBe(false);
    // Without a base, any written status_id counts as changed (conservative).
    expect(issueChangedDims({ status_id: "cat-b" }).status).toBe(true);
  });

  it("ignores non-membership fields", () => {
    expect(issueChangedDims({ title: "x", position: 9 })).toEqual({
      assignee: false,
      project: false,
      status: false,
    });
  });
});

describe("listFilterDependsOn", () => {
  const none = { assignee: false, project: false, status: false };

  it("my:all reacts to assignee changes only", () => {
    expect(listFilterDependsOn("all", {}, { ...none, assignee: true })).toBe(true);
    expect(listFilterDependsOn("all", {}, { ...none, project: true })).toBe(false);
  });

  it("assignee-keyed filters react to assignee changes", () => {
    expect(
      listFilterDependsOn("assigned", { assignee_id: "me" }, { ...none, assignee: true }),
    ).toBe(true);
    expect(
      listFilterDependsOn(
        "workspace:members",
        { assignee_types: ["member"] },
        { ...none, assignee: true },
      ),
    ).toBe(true);
    expect(
      listFilterDependsOn("agents", { involves_user_id: "me" }, { ...none, assignee: true }),
    ).toBe(true);
    expect(
      listFilterDependsOn("assigned", { assignee_id: "me" }, { ...none, project: true }),
    ).toBe(false);
  });

  it("project filters react to project changes", () => {
    expect(
      listFilterDependsOn("project:p1", { project_id: "p1" }, { ...none, project: true }),
    ).toBe(true);
    expect(
      listFilterDependsOn("project:p1", { project_id: "p1" }, { ...none, assignee: true }),
    ).toBe(false);
  });

  it("creator filters never react — creator is immutable", () => {
    expect(
      listFilterDependsOn(
        "created",
        { creator_id: "me" },
        { assignee: true, project: true, status: true },
      ),
    ).toBe(false);
  });

  it("the unfiltered workspace list never reacts", () => {
    expect(
      listFilterDependsOn(undefined, {}, { assignee: true, project: true, status: true }),
    ).toBe(false);
  });
});
