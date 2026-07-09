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
      priority: false,
    });
    expect(issueChangedDims({ project_id: null })).toEqual({
      assignee: false,
      project: true,
      status: false,
      priority: false,
    });
  });

  it("sharpens against a base entity — writing the same value changes nothing", () => {
    const base = makeIssue();
    expect(issueChangedDims({ assignee_id: "me", assignee_type: "member" }, base)).toEqual({
      assignee: false,
      project: false,
      status: false,
      priority: false,
    });
    expect(issueChangedDims({ status: "todo" }, base).status).toBe(false);
    expect(issueChangedDims({ status: "done" }, base).status).toBe(true);
    expect(issueChangedDims({ project_id: "p2" }, base).project).toBe(true);
    expect(issueChangedDims({ priority: "none" }, base).priority).toBe(false);
    expect(issueChangedDims({ priority: "high" }, base).priority).toBe(true);
    // Without a base, any written priority counts as changed.
    expect(issueChangedDims({ priority: "high" }).priority).toBe(true);
  });

  it("ignores non-membership fields", () => {
    expect(issueChangedDims({ title: "x", position: 9 })).toEqual({
      assignee: false,
      project: false,
      status: false,
      priority: false,
    });
  });
});

describe("listFilterDependsOn", () => {
  const none = { assignee: false, project: false, status: false, priority: false };

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
        { assignee: true, project: true, status: true, priority: false },
      ),
    ).toBe(false);
  });

  it("the unfiltered workspace list never reacts", () => {
    expect(
      listFilterDependsOn(undefined, {}, {
        assignee: true,
        project: true,
        status: true,
        priority: false,
      }),
    ).toBe(false);
  });

  it("priority-filtered lists react to priority changes (workspace + my:all)", () => {
    // Workspace list with a priority facet: a priority change moves membership.
    expect(
      listFilterDependsOn(undefined, { priorities: ["high"] }, { ...none, priority: true }),
    ).toBe(true);
    // No priority facet → a priority change is a no-op for the workspace list.
    expect(
      listFilterDependsOn(undefined, {}, { ...none, priority: true }),
    ).toBe(false);
    // my:all is normally assignee-only, but a priority facet layered on top
    // makes it react to priority changes too (the all-scope shortcut must not
    // swallow the priority dimension).
    expect(
      listFilterDependsOn("all", { priorities: ["high"] }, { ...none, priority: true }),
    ).toBe(true);
    expect(listFilterDependsOn("all", {}, { ...none, priority: true })).toBe(false);
  });
});

describe("issueMatchesListFilter — priorities", () => {
  it("judges priority membership definitively from the entity", () => {
    expect(
      issueMatchesListFilter(makeIssue({ priority: "high" }), undefined, {
        priorities: ["high", "urgent"],
      }),
    ).toBe(true);
    expect(
      issueMatchesListFilter(makeIssue({ priority: "low" }), undefined, {
        priorities: ["high", "urgent"],
      }),
    ).toBe(false);
  });

  it("a priority miss is a definitive non-member even for the my:all union", () => {
    // The all-scope early return must NOT bypass the priority check: a low
    // issue can never belong to a priorities:["high"] all-list, regardless of
    // the server-only union membership.
    expect(
      issueMatchesListFilter(makeIssue({ priority: "low" }), "all", {
        priorities: ["high"],
      }),
    ).toBe(false);
    // A priority match still leaves the union itself undecidable.
    expect(
      issueMatchesListFilter(makeIssue({ priority: "high" }), "all", {
        priorities: ["high"],
      }),
    ).toBe("unknown");
  });

  it("degrades to unknown when a partial entity is missing priority", () => {
    expect(
      issueMatchesListFilter({ title: "partial" }, undefined, { priorities: ["high"] }),
    ).toBe("unknown");
  });
});
