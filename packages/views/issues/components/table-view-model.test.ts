import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import {
  buildIssueTableCsv,
  calculateIssueTableColumn,
  getIssueTableSelectionRange,
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
