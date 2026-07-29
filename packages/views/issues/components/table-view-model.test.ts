import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import {
  buildIssueTableCsv,
  calculateIssueTableColumn,
  getIssueTableSelectionRange,
  refreshFrozenTableRows,
  type IssueTableDisplayRow,
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

describe("refreshFrozenTableRows", () => {
  const groupRow: IssueTableDisplayRow = {
    kind: "group",
    key: "status:todo",
    label: "Todo",
    count: 2,
    collapsed: false,
  };

  it("keeps structure and keys while swapping in live issue objects", () => {
    const staleA = makeIssue("issue-1", { title: "Stale A" });
    const staleB = makeIssue("issue-2", { title: "Stale B" });
    const snapshot: IssueTableDisplayRow[] = [
      groupRow,
      {
        kind: "issue",
        key: staleA.id,
        issue: staleA,
        depth: 0,
        hasChildren: true,
        collapsed: false,
      },
      {
        kind: "issue",
        key: staleB.id,
        issue: staleB,
        depth: 1,
        hasChildren: false,
        collapsed: false,
      },
    ];
    const liveA = makeIssue("issue-1", { title: "Live A" });

    const refreshed = refreshFrozenTableRows(
      snapshot,
      new Map([
        [liveA.id, liveA],
        [staleB.id, staleB],
      ]),
    );

    expect(refreshed.map((row) => row.key)).toEqual([
      "status:todo",
      "issue-1",
      "issue-2",
    ]);
    expect(refreshed[1]).toMatchObject({
      issue: liveA,
      depth: 0,
      hasChildren: true,
    });
    expect(refreshed[2]).toBe(snapshot[2]);
    expect(refreshed[0]).toBe(groupRow);
  });

  it("keeps the stale issue when it vanished from the live window", () => {
    const stale = makeIssue("issue-9", { title: "Deleted remotely" });
    const snapshot: IssueTableDisplayRow[] = [
      {
        kind: "issue",
        key: stale.id,
        issue: stale,
        depth: 0,
        hasChildren: false,
        collapsed: false,
      },
    ];

    const refreshed = refreshFrozenTableRows(snapshot, new Map());

    expect(refreshed[0]).toBe(snapshot[0]);
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
