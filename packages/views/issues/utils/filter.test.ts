import { describe, it, expect } from "vitest";
import { buildIssueListFilter, type IssueViewFilters } from "./filter";

const NO_FILTER: IssueViewFilters = {
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
};

describe("buildIssueListFilter", () => {
  it("returns an empty object when nothing is selected", () => {
    expect(buildIssueListFilter(NO_FILTER)).toEqual({});
  });

  it("encodes priority filter as an array", () => {
    expect(
      buildIssueListFilter({ ...NO_FILTER, priorityFilters: ["high", "urgent"] }),
    ).toEqual({ priorities: ["high", "urgent"] });
  });

  it("encodes assignee filters as id-only arrays", () => {
    expect(
      buildIssueListFilter({
        ...NO_FILTER,
        assigneeFilters: [
          { type: "member", id: "u-1" },
          { type: "agent", id: "a-1" },
        ],
      }),
    ).toEqual({ assignee_ids: ["u-1", "a-1"] });
  });

  it("encodes 'no assignee' as include_no_assignee", () => {
    expect(buildIssueListFilter({ ...NO_FILTER, includeNoAssignee: true })).toEqual({
      include_no_assignee: true,
    });
  });

  it("combines assignee ids with the no-assignee toggle", () => {
    expect(
      buildIssueListFilter({
        ...NO_FILTER,
        assigneeFilters: [{ type: "member", id: "u-1" }],
        includeNoAssignee: true,
      }),
    ).toEqual({ assignee_ids: ["u-1"], include_no_assignee: true });
  });

  it("encodes creator and project filters", () => {
    expect(
      buildIssueListFilter({
        ...NO_FILTER,
        creatorFilters: [{ type: "member", id: "u-2" }],
        projectFilters: ["p-1", "p-2"],
        includeNoProject: true,
      }),
    ).toEqual({
      creator_ids: ["u-2"],
      project_ids: ["p-1", "p-2"],
      include_no_project: true,
    });
  });

  it("encodes label filters", () => {
    expect(
      buildIssueListFilter({ ...NO_FILTER, labelFilters: ["l-1", "l-2"] }),
    ).toEqual({ label_ids: ["l-1", "l-2"] });
  });

  it("combines every filter dimension", () => {
    expect(
      buildIssueListFilter({
        priorityFilters: ["high"],
        assigneeFilters: [{ type: "member", id: "u-1" }],
        includeNoAssignee: false,
        creatorFilters: [{ type: "agent", id: "a-1" }],
        projectFilters: ["p-1"],
        includeNoProject: false,
        labelFilters: ["l-1"],
      }),
    ).toEqual({
      priorities: ["high"],
      assignee_ids: ["u-1"],
      creator_ids: ["a-1"],
      project_ids: ["p-1"],
      label_ids: ["l-1"],
    });
  });
});
