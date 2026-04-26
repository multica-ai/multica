import { describe, it, expect } from "vitest";
import type { Issue, IssueLabel } from "@multica/core/types";
import { filterIssues, type IssueFilters } from "./filter";

const NO_FILTER: IssueFilters = {
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
  includeNoLabels: false,
};

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "i-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Test",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "u-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    due_date: null,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    labels: [],
    ...overrides,
  };
}

const issues: Issue[] = [
  makeIssue({ id: "1", status: "todo", priority: "high", assignee_type: "member", assignee_id: "u-1", creator_type: "member", creator_id: "u-1", project_id: "p-1" }),
  makeIssue({ id: "2", status: "in_progress", priority: "medium", assignee_type: "agent", assignee_id: "a-1", creator_type: "agent", creator_id: "a-1", project_id: "p-2" }),
  makeIssue({ id: "3", status: "done", priority: "low", assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "u-2", project_id: null }),
  makeIssue({ id: "4", status: "todo", priority: "urgent", assignee_type: "member", assignee_id: "u-2", creator_type: "member", creator_id: "u-1", project_id: "p-1" }),
];

describe("filterIssues", () => {
  it("returns all issues when no filters are active", () => {
    expect(filterIssues(issues, NO_FILTER)).toHaveLength(4);
  });

  // --- Status ---
  it("filters by status", () => {
    const result = filterIssues(issues, { ...NO_FILTER, statusFilters: ["todo"] });
    expect(result.map((i) => i.id)).toEqual(["1", "4"]);
  });

  // --- Priority ---
  it("filters by priority", () => {
    const result = filterIssues(issues, { ...NO_FILTER, priorityFilters: ["high", "urgent"] });
    expect(result.map((i) => i.id)).toEqual(["1", "4"]);
  });

  // --- Assignee ---
  it("filters by specific assignee", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      assigneeFilters: [{ type: "member", id: "u-1" }],
    });
    expect(result.map((i) => i.id)).toEqual(["1"]);
  });

  it("filters by 'No assignee' only", () => {
    const result = filterIssues(issues, { ...NO_FILTER, includeNoAssignee: true });
    expect(result.map((i) => i.id)).toEqual(["3"]);
  });

  it("filters by assignee + No assignee combined", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      assigneeFilters: [{ type: "agent", id: "a-1" }],
      includeNoAssignee: true,
    });
    expect(result.map((i) => i.id)).toEqual(["2", "3"]);
  });

  it("hides assigned issues when only 'No assignee' is selected", () => {
    const result = filterIssues(issues, { ...NO_FILTER, includeNoAssignee: true });
    expect(result.every((i) => !i.assignee_id)).toBe(true);
  });

  // --- Creator ---
  it("filters by creator", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      creatorFilters: [{ type: "agent", id: "a-1" }],
    });
    expect(result.map((i) => i.id)).toEqual(["2"]);
  });

  // --- Combinations ---
  it("applies status + assignee filters together", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      assigneeFilters: [{ type: "member", id: "u-1" }],
    });
    expect(result.map((i) => i.id)).toEqual(["1"]);
  });

  it("applies status + priority + creator filters together", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      priorityFilters: ["urgent"],
      creatorFilters: [{ type: "member", id: "u-1" }],
    });
    expect(result.map((i) => i.id)).toEqual(["4"]);
  });

  // --- Project ---
  it("filters by specific project", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      projectFilters: ["p-1"],
    });
    expect(result.map((i) => i.id)).toEqual(["1", "4"]);
  });

  it("filters by multiple projects", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      projectFilters: ["p-1", "p-2"],
    });
    expect(result.map((i) => i.id)).toEqual(["1", "2", "4"]);
  });

  it("filters by 'No project' only", () => {
    const result = filterIssues(issues, { ...NO_FILTER, includeNoProject: true });
    expect(result.map((i) => i.id)).toEqual(["3"]);
  });

  it("filters by project + No project combined", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      projectFilters: ["p-2"],
      includeNoProject: true,
    });
    expect(result.map((i) => i.id)).toEqual(["2", "3"]);
  });

  it("hides project issues when only 'No project' is selected", () => {
    const result = filterIssues(issues, { ...NO_FILTER, includeNoProject: true });
    expect(result.every((i) => !i.project_id)).toBe(true);
  });

  it("applies status + project filters together", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      projectFilters: ["p-1"],
    });
    expect(result.map((i) => i.id)).toEqual(["1", "4"]);
  });
});


// ---------------------------------------------------------------------------
// Label filtering tests
// ---------------------------------------------------------------------------

const labelA: IssueLabel = { id: "lbl-a", workspace_id: "ws-1", name: "bug", color: "red", creator_type: "member", creator_id: "u-1", created_at: "2025-01-01T00:00:00Z", updated_at: "2025-01-01T00:00:00Z" };
const labelB: IssueLabel = { id: "lbl-b", workspace_id: "ws-1", name: "ui", color: "blue", creator_type: "member", creator_id: "u-1", created_at: "2025-01-01T00:00:00Z", updated_at: "2025-01-01T00:00:00Z" };
const labelC: IssueLabel = { id: "lbl-c", workspace_id: "ws-1", name: "infra", color: "green", creator_type: "member", creator_id: "u-1", created_at: "2025-01-01T00:00:00Z", updated_at: "2025-01-01T00:00:00Z" };

const labeledIssues: Issue[] = [
  makeIssue({ id: "L1", labels: [labelA] }),
  makeIssue({ id: "L2", labels: [labelB] }),
  makeIssue({ id: "L3", labels: [labelA, labelB] }),
  makeIssue({ id: "L4", labels: [] }),
  makeIssue({ id: "L5", labels: [labelC] }),
];

describe("filterIssues — labels", () => {
  it("returns all issues when labelFilters is empty and includeNoLabels is false", () => {
    expect(filterIssues(labeledIssues, NO_FILTER).map((i) => i.id)).toEqual(["L1", "L2", "L3", "L4", "L5"]);
  });

  it("filters by labelFilters one match (OR semantics, 1 id)", () => {
    expect(
      filterIssues(labeledIssues, { ...NO_FILTER, labelFilters: ["lbl-a"] }).map((i) => i.id),
    ).toEqual(["L1", "L3"]);
  });

  it("filters by labelFilters with OR across multiple ids", () => {
    expect(
      filterIssues(labeledIssues, { ...NO_FILTER, labelFilters: ["lbl-a", "lbl-c"] }).map((i) => i.id),
    ).toEqual(["L1", "L3", "L5"]);
  });

  it("returns only un-labeled issues when includeNoLabels is true alone", () => {
    expect(
      filterIssues(labeledIssues, { ...NO_FILTER, includeNoLabels: true }).map((i) => i.id),
    ).toEqual(["L4"]);
  });

  it("combines labelFilters with includeNoLabels (label OR no-label)", () => {
    expect(
      filterIssues(labeledIssues, {
        ...NO_FILTER,
        labelFilters: ["lbl-c"],
        includeNoLabels: true,
      }).map((i) => i.id),
    ).toEqual(["L4", "L5"]);
  });

  it("composes label filters with status filters (AND across dimensions)", () => {
    const issues: Issue[] = [
      makeIssue({ id: "X1", status: "todo", labels: [labelA] }),
      makeIssue({ id: "X2", status: "done", labels: [labelA] }),
      makeIssue({ id: "X3", status: "todo", labels: [labelB] }),
    ];
    expect(
      filterIssues(issues, {
        ...NO_FILTER,
        statusFilters: ["todo"],
        labelFilters: ["lbl-a"],
      }).map((i) => i.id),
    ).toEqual(["X1"]);
  });
});

