import { describe, it, expect } from "vitest";
import type { Issue, IssueAssigneeGroup } from "@multica/core/types";
import {
  applyIssueFilters,
  filterAssigneeGroups,
  filterIssues,
  type IssueFilters,
} from "./filter";

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
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
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

  it("treats an explicitly active empty assignee predicate as match-none", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      assigneeFilterActive: true,
    });
    expect(result).toEqual([]);
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

  // --- Agent running quick filter ---
  it("keeps only running issues when agentRunningFilter is on", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      agentRunningFilter: true,
      runningIssueIds: new Set(["2", "4"]),
    });
    expect(result.map((i) => i.id)).toEqual(["2", "4"]);
  });

  it("hides everything when agentRunningFilter is on but no ids running", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      agentRunningFilter: true,
      runningIssueIds: new Set(),
    });
    expect(result).toHaveLength(0);
  });

  it("ignores runningIssueIds when agentRunningFilter is off", () => {
    // The set is irrelevant unless the toggle is true — this guards against
    // a future refactor accidentally applying the set as an implicit
    // pre-filter when the user hasn't asked for it.
    const result = filterIssues(issues, {
      ...NO_FILTER,
      runningIssueIds: new Set(["2"]),
    });
    expect(result).toHaveLength(4);
  });

  it("composes agentRunningFilter with other filters (AND semantics)", () => {
    const result = filterIssues(issues, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      agentRunningFilter: true,
      runningIssueIds: new Set(["1", "2"]),
    });
    // Issue 2 is in_progress (filtered out by status), issue 1 is todo and
    // in the running set → only "1" survives.
    expect(result.map((i) => i.id)).toEqual(["1"]);
  });

  it("applies workingOnly from activity context without treating queued issues as working", () => {
    const result = applyIssueFilters(
      issues,
      {
        ...NO_FILTER,
        workingOnly: true,
      },
      {
        activityByIssueId: new Map([
          ["1", { isWorking: true, isQueued: false, runningTasks: [], queuedTasks: [] }],
          ["2", { isWorking: false, isQueued: true, runningTasks: [], queuedTasks: [] }],
        ]),
      },
    );

    expect(result.map((i) => i.id)).toEqual(["1"]);
  });

  // --- Show sub-issues display toggle ---
  const parentChildIssues: Issue[] = [
    makeIssue({ id: "P1", parent_issue_id: null }),
    makeIssue({ id: "C1", parent_issue_id: "P1" }),
    makeIssue({ id: "P2", parent_issue_id: null }),
    makeIssue({ id: "C2", parent_issue_id: "P2" }),
  ];

  it("hides sub-issues when showSubIssues is false", () => {
    const result = filterIssues(parentChildIssues, {
      ...NO_FILTER,
      showSubIssues: false,
    });
    expect(result.map((i) => i.id)).toEqual(["P1", "P2"]);
  });

  it("keeps sub-issues when showSubIssues is true or omitted", () => {
    expect(
      filterIssues(parentChildIssues, { ...NO_FILTER, showSubIssues: true }),
    ).toHaveLength(4);
    // Omitting the flag entirely must preserve the show-all default.
    expect(filterIssues(parentChildIssues, NO_FILTER)).toHaveLength(4);
  });

  it("composes showSubIssues with other filters (AND semantics)", () => {
    const mixed: Issue[] = [
      makeIssue({ id: "P", status: "todo", parent_issue_id: null }),
      makeIssue({ id: "C", status: "todo", parent_issue_id: "P" }),
      makeIssue({ id: "PD", status: "done", parent_issue_id: null }),
    ];
    const result = filterIssues(mixed, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      showSubIssues: false,
    });
    // "C" is a sub-issue (dropped), "PD" is done (dropped) → only "P" survives.
    expect(result.map((i) => i.id)).toEqual(["P"]);
  });
});

describe("filterAssigneeGroups", () => {
  const group = (id: string, groupIssues: Issue[]): IssueAssigneeGroup => ({
    id,
    assignee_type: id === "none" ? null : "agent",
    assignee_id: id === "none" ? null : id,
    issues: groupIssues,
    total: groupIssues.length,
  });

  it("returns the same reference when no client-side filter is active", () => {
    const groups = [group("a1", [makeIssue({ id: "1" })])];
    expect(filterAssigneeGroups(groups, {})).toBe(groups);
    expect(filterAssigneeGroups(groups, { showSubIssues: true })).toBe(groups);
    expect(filterAssigneeGroups(groups, { agentRunningFilter: false })).toBe(groups);
  });

  it("passes undefined through untouched", () => {
    expect(filterAssigneeGroups(undefined, { showSubIssues: false })).toBeUndefined();
  });

  it("hides sub-issues, recomputes total, and drops emptied groups", () => {
    const groups = [
      group("a1", [
        makeIssue({ id: "P1", parent_issue_id: null }),
        makeIssue({ id: "C1", parent_issue_id: "P1" }),
      ]),
      // Every issue in this group is a sub-issue → group is removed entirely.
      group("a2", [makeIssue({ id: "C2", parent_issue_id: "P2" })]),
    ];
    const result = filterAssigneeGroups(groups, { showSubIssues: false });
    expect(
      result!.map((g) => ({ id: g.id, ids: g.issues.map((i) => i.id), total: g.total })),
    ).toEqual([{ id: "a1", ids: ["P1"], total: 1 }]);
  });

  it("keeps only running issues when agentRunningFilter is on", () => {
    const groups = [
      group("a1", [makeIssue({ id: "1" }), makeIssue({ id: "2" })]),
      group("a2", [makeIssue({ id: "3" })]),
      group("none", [makeIssue({ id: "4" })]),
    ];
    const result = filterAssigneeGroups(groups, {
      agentRunningFilter: true,
      runningIssueIds: new Set(["2", "4"]),
    });
    expect(
      result!.map((g) => ({ id: g.id, ids: g.issues.map((i) => i.id), total: g.total })),
    ).toEqual([
      { id: "a1", ids: ["2"], total: 1 },
      { id: "none", ids: ["4"], total: 1 },
    ]);
  });

  it("composes showSubIssues and agentRunningFilter (AND semantics)", () => {
    const groups = [
      group("a1", [
        makeIssue({ id: "P", parent_issue_id: null }),
        makeIssue({ id: "C", parent_issue_id: "P" }),
      ]),
    ];
    // "C" is running but is a sub-issue; "P" is a top-level issue but not
    // running → both dropped, group removed.
    const result = filterAssigneeGroups(groups, {
      showSubIssues: false,
      agentRunningFilter: true,
      runningIssueIds: new Set(["C"]),
    });
    expect(result).toEqual([]);
  });
});

describe("property filters", () => {
  const sevId = "prop-severity";
  const platId = "prop-platforms";
  const doneId = "prop-done";
  const critical = makeIssue({ id: "P1", properties: { [sevId]: "opt-critical" } });
  const minor = makeIssue({ id: "P2", properties: { [sevId]: "opt-minor", [platId]: ["opt-ios", "opt-web"] } });
  const unset = makeIssue({ id: "P3" });
  const checked = makeIssue({ id: "P4", properties: { [doneId]: true } });

  it("select values match by option id (OR within the definition)", () => {
    const result = filterIssues([critical, minor, unset], {
      ...NO_FILTER,
      propertyFilters: { [sevId]: ["opt-critical", "opt-minor"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P1", "P2"]);
  });

  it("issues without a value never match a filtered definition", () => {
    const result = filterIssues([critical, unset], {
      ...NO_FILTER,
      propertyFilters: { [sevId]: ["opt-critical"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P1"]);
  });

  it("multi_select matches on intersection", () => {
    const result = filterIssues([critical, minor], {
      ...NO_FILTER,
      propertyFilters: { [platId]: ["opt-web"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P2"]);
  });

  it("checkbox values match the true/false pseudo-options", () => {
    const result = filterIssues([checked, unset], {
      ...NO_FILTER,
      propertyFilters: { [doneId]: ["true"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P4"]);
  });

  it("ANDs across definitions", () => {
    const result = filterIssues([critical, minor], {
      ...NO_FILTER,
      propertyFilters: { [sevId]: ["opt-minor"], [platId]: ["opt-ios"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P2"]);
  });

  it("empty selections are inert", () => {
    const result = filterIssues([critical, minor, unset], {
      ...NO_FILTER,
      propertyFilters: { [sevId]: [] },
    });
    expect(result).toHaveLength(3);
  });

  it("filterAssigneeGroups applies property filters per group", () => {
    const groups: IssueAssigneeGroup[] = [
      { id: "assignee:member:u-1", assignee_type: "member", assignee_id: "u-1", issues: [critical, minor], total: 2 },
    ];
    const result = filterAssigneeGroups(groups, {
      propertyFilters: { [sevId]: ["opt-critical"] },
    });
    expect(result?.[0]?.issues.map((i) => i.id)).toEqual(["P1"]);
    expect(result?.[0]?.total).toBe(1);
  });
});
