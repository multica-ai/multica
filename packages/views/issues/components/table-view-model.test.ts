import { describe, expect, it } from "vitest";
import type { Issue, IssueProperty } from "@multica/core/types";
import {
  TABLE_STRUCTURE_MAX_WINDOW,
  buildIssueTableCsv,
  buildIssueTableRows,
  calculateIssueTableColumn,
  getIssueTableSelectionRange,
  isTableStructureSuspended,
  shouldAutoLoadNextWindowPage,
} from "./table-view-model";

function makeIssue(id: string, overrides: Partial<Issue> = {}): Issue {
  const number = Number(id.replace(/\D/g, "")) || 1;
  return {
    id,
    workspace_id: "ws-1",
    number,
    identifier: `MUL-${number}`,
    title: `Issue ${id}`,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: number,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeProperty(
  id: string,
  type: string,
  options: IssueProperty["config"]["options"] = [],
): IssueProperty {
  return {
    id,
    workspace_id: "ws-1",
    name: id,
    type,
    config: { options },
    position: 0,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

const baseOptions = {
  grouping: "none" as const,
  properties: [] as IssueProperty[],
  collapsedGroups: new Set<string>(),
  collapsedParents: new Set<string>(),
  hierarchy: true,
  windowComplete: true,
  getActorName: (_type: string, id: string) => id,
  getStatusLabel: (status: Issue["status"]) => status,
  noValueLabel: "No value",
  unassignedLabel: "Unassigned",
  trueLabel: "Yes",
  falseLabel: "No",
};

describe("isTableStructureSuspended", () => {
  it("suspends structure only above the ceiling; unknown totals do not suspend", () => {
    expect(isTableStructureSuspended(0)).toBe(false);
    expect(isTableStructureSuspended(TABLE_STRUCTURE_MAX_WINDOW)).toBe(false);
    expect(isTableStructureSuspended(TABLE_STRUCTURE_MAX_WINDOW + 1)).toBe(true);
  });
});

describe("shouldAutoLoadNextWindowPage", () => {
  const base = {
    windowWanted: true,
    total: 500,
    loadedCount: 100,
    hasNextPage: true,
    isFetchingNextPage: false,
    hasError: false,
  };

  it("advances only while structure is wanted, healthy, and under the ceiling", () => {
    expect(shouldAutoLoadNextWindowPage(base)).toBe(true);
    expect(
      shouldAutoLoadNextWindowPage({ ...base, windowWanted: false }),
    ).toBe(false);
    expect(shouldAutoLoadNextWindowPage({ ...base, hasNextPage: false })).toBe(
      false,
    );
    expect(
      shouldAutoLoadNextWindowPage({ ...base, isFetchingNextPage: true }),
    ).toBe(false);
  });

  it("stops permanently on a fetch error instead of refiring (request storm)", () => {
    // After a failed page fetch, hasNextPage stays true and
    // isFetchingNextPage returns to false — exactly the state that used to
    // re-trigger the loop forever.
    expect(shouldAutoLoadNextWindowPage({ ...base, hasError: true })).toBe(
      false,
    );
  });

  it("hard-stops at the ceiling even when a stale total says otherwise", () => {
    // page 1 reported total=900 (under the ceiling) but the window has since
    // grown to 50,000: the fresh total suspends...
    expect(
      shouldAutoLoadNextWindowPage({ ...base, total: 50_000 }),
    ).toBe(false);
    // ...and independently of ANY reported total, the loaded count is an
    // absolute stop at the ceiling — page 3 must never be requested once
    // 1,000 rows are in memory.
    expect(
      shouldAutoLoadNextWindowPage({
        ...base,
        total: 900,
        loadedCount: TABLE_STRUCTURE_MAX_WINDOW,
      }),
    ).toBe(false);
  });
});

describe("buildIssueTableRows", () => {
  it("renders a parent before its children and honors parent collapse", () => {
    const parent = makeIssue("issue-1");
    const child = makeIssue("issue-2", { parent_issue_id: parent.id });

    const expanded = buildIssueTableRows([child, parent], baseOptions);
    expect(
      expanded.map((row) =>
        row.kind === "issue" ? [row.issue.id, row.depth] : row.key,
      ),
    ).toEqual([
      ["issue-1", 0],
      ["issue-2", 1],
    ]);

    const collapsed = buildIssueTableRows([child, parent], {
      ...baseOptions,
      collapsedParents: new Set([parent.id]),
    });
    expect(collapsed.map((row) => row.key)).toEqual([parent.id]);
  });

  it("suspends hierarchy and parent-based grouping while the window is incomplete", () => {
    const parent = makeIssue("issue-1", { status: "done" });
    const child = makeIssue("issue-2", {
      parent_issue_id: parent.id,
      status: "todo",
    });

    // Nesting derived from loaded pages re-parents rows as later pages
    // arrive; rows must stay in flat sort order until the window completes.
    const flat = buildIssueTableRows([child, parent], {
      ...baseOptions,
      windowComplete: false,
    });
    expect(
      flat.map((row) =>
        row.kind === "issue" ? [row.issue.id, row.depth] : row.key,
      ),
    ).toEqual([
      ["issue-2", 0],
      ["issue-1", 0],
    ]);

    // Grouped: the child buckets by its OWN status instead of climbing to
    // the parent, so its group cannot change when the parent pages in.
    const grouped = buildIssueTableRows([child, parent], {
      ...baseOptions,
      grouping: "status",
      windowComplete: false,
    });
    expect(grouped.map((row) => row.key)).toEqual([
      "status:todo",
      child.id,
      "status:done",
      parent.id,
    ]);
  });

  it("keeps a hierarchy together under the parent custom-property group", () => {
    const environment = makeProperty("environment", "select", [
      { id: "web", name: "Web", color: "#000000" },
      { id: "mobile", name: "Mobile", color: "#ffffff" },
    ]);
    const parent = makeIssue("issue-1", {
      properties: { environment: "web" },
    });
    const child = makeIssue("issue-2", {
      parent_issue_id: parent.id,
      properties: { environment: "mobile" },
    });

    const rows = buildIssueTableRows([parent, child], {
      ...baseOptions,
      grouping: "property:environment",
      properties: [environment],
    });

    expect(rows[0]).toMatchObject({ kind: "group", label: "Web", count: 2 });
    expect(rows.map((row) => row.key)).toEqual([
      "property:environment:web",
      parent.id,
      child.id,
    ]);
  });

  it("orders status groups canonically and honors group collapse", () => {
    const rows = buildIssueTableRows(
      [
        makeIssue("issue-1", { status: "done" }),
        makeIssue("issue-2", { status: "backlog" }),
      ],
      {
        ...baseOptions,
        grouping: "status",
        collapsedGroups: new Set(["status:backlog"]),
      },
    );

    expect(rows.map((row) => row.key)).toEqual([
      "status:backlog",
      "status:done",
      "issue-1",
    ]);
  });
});

describe("getIssueTableSelectionRange", () => {
  const issueIds = ["issue-1", "issue-2", "issue-3", "issue-4"];

  it("returns an inclusive range in either direction", () => {
    expect(
      getIssueTableSelectionRange(issueIds, "issue-1", "issue-4"),
    ).toEqual(issueIds);
    expect(
      getIssueTableSelectionRange(issueIds, "issue-4", "issue-2"),
    ).toEqual(["issue-2", "issue-3", "issue-4"]);
  });

  it("returns null when the anchor or target is not visible", () => {
    expect(getIssueTableSelectionRange(issueIds, null, "issue-2")).toBeNull();
    expect(
      getIssueTableSelectionRange(issueIds, "missing", "issue-2"),
    ).toBeNull();
    expect(
      getIssueTableSelectionRange(issueIds, "issue-1", "missing"),
    ).toBeNull();
  });
});

describe("table calculations and CSV", () => {
  it("calculates numeric custom-property sums, averages, and counts", () => {
    const issues = [
      makeIssue("issue-1", { properties: { estimate: 3 } }),
      makeIssue("issue-2", { properties: { estimate: 5 } }),
      makeIssue("issue-3"),
    ];

    expect(
      calculateIssueTableColumn(issues, "property:estimate", "sum"),
    ).toBe(8);
    expect(
      calculateIssueTableColumn(issues, "property:estimate", "average"),
    ).toBe(4);
    expect(
      calculateIssueTableColumn(issues, "property:estimate", "count"),
    ).toBe(2);
  });

  it("escapes commas, quotes, and newlines in CSV output", () => {
    expect(
      buildIssueTableCsv(
        ["Identifier", "Title"],
        [["MUL-1", 'Ship, "verify"\nnext']],
      ),
    ).toBe('Identifier,Title\r\nMUL-1,"Ship, ""verify""\nnext"');
  });

  it("neutralizes spreadsheet formulas in headers and string cells", () => {
    expect(
      buildIssueTableCsv(
        ["=Injected", "Value"],
        [["+SUM(A1:A2)", -42], ["\tcmd", "@remote"]],
      ),
    ).toBe(
      "'=Injected,Value\r\n'+SUM(A1:A2),-42\r\n'\tcmd,'@remote",
    );
  });
});
