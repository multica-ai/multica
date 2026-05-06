import { describe, it, expect } from "vitest";
import type { Issue } from "@multica/core/types";
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

  // --- Label ---
  // Build a separate fixture for label tests so we can sprinkle labels onto
  // specific rows without polluting the assignee/project test cases above.
  const makeLabel = (id: string, name: string, color: string) => ({
    id,
    name,
    color,
    instructions: "",
    workspace_id: "ws-1",
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  });
  const labelBug = makeLabel("lab-bug", "bug", "#ff0000");
  const labelFeat = makeLabel("lab-feat", "feature", "#00ff00");
  const labelP0 = makeLabel("lab-p0", "p0", "#0000ff");
  const labeledIssues: Issue[] = [
    makeIssue({ id: "L1", labels: [labelBug] }),
    makeIssue({ id: "L2", labels: [labelFeat] }),
    makeIssue({ id: "L3", labels: [labelBug, labelP0] }),
    makeIssue({ id: "L4", labels: [] }),
    makeIssue({ id: "L5" }), // labels field absent
  ];

  it("filters by a single label", () => {
    const result = filterIssues(labeledIssues, { ...NO_FILTER, labelFilters: ["lab-bug"] });
    expect(result.map((i) => i.id)).toEqual(["L1", "L3"]);
  });

  it("filters by multiple labels with OR semantics", () => {
    const result = filterIssues(labeledIssues, {
      ...NO_FILTER,
      labelFilters: ["lab-bug", "lab-feat"],
    });
    expect(result.map((i) => i.id)).toEqual(["L1", "L2", "L3"]);
  });

  it("excludes issues with no labels when a label filter is active", () => {
    const result = filterIssues(labeledIssues, { ...NO_FILTER, labelFilters: ["lab-bug"] });
    // L4 (empty labels) and L5 (missing labels field) must both be filtered out.
    expect(result.map((i) => i.id)).not.toContain("L4");
    expect(result.map((i) => i.id)).not.toContain("L5");
  });
});
