import { describe, it, expect } from "vitest";
import type { Issue } from "@multica/core/types";
import { sortIssues } from "./sort";

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
    start_date: null,
    due_date: null,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("sortIssues", () => {
  it("sorts each field in ascending order", () => {
    const cases = [
      {
        field: "priority" as const,
        issues: [
          makeIssue({ id: "low", priority: "low" }),
          makeIssue({ id: "urgent", priority: "urgent" }),
          makeIssue({ id: "medium", priority: "medium" }),
        ],
        expected: ["urgent", "medium", "low"],
      },
      {
        field: "start_date" as const,
        issues: [
          makeIssue({ id: "late", start_date: "2025-03-01T00:00:00Z" }),
          makeIssue({ id: "early", start_date: "2025-01-01T00:00:00Z" }),
          makeIssue({ id: "mid", start_date: "2025-02-01T00:00:00Z" }),
        ],
        expected: ["early", "mid", "late"],
      },
      {
        field: "due_date" as const,
        issues: [
          makeIssue({ id: "late", due_date: "2025-03-01T00:00:00Z" }),
          makeIssue({ id: "early", due_date: "2025-01-01T00:00:00Z" }),
          makeIssue({ id: "mid", due_date: "2025-02-01T00:00:00Z" }),
        ],
        expected: ["early", "mid", "late"],
      },
      {
        field: "created_at" as const,
        issues: [
          makeIssue({ id: "late", created_at: "2025-03-01T00:00:00Z" }),
          makeIssue({ id: "early", created_at: "2025-01-01T00:00:00Z" }),
          makeIssue({ id: "mid", created_at: "2025-02-01T00:00:00Z" }),
        ],
        expected: ["early", "mid", "late"],
      },
      {
        field: "title" as const,
        issues: [
          makeIssue({ id: "charlie", title: "Charlie" }),
          makeIssue({ id: "alpha", title: "Alpha" }),
          makeIssue({ id: "bravo", title: "Bravo" }),
        ],
        expected: ["alpha", "bravo", "charlie"],
      },
      {
        field: "position" as const,
        issues: [
          makeIssue({ id: "third", position: 30 }),
          makeIssue({ id: "first", position: 10 }),
          makeIssue({ id: "second", position: 20 }),
        ],
        expected: ["first", "second", "third"],
      },
    ];

    expect(
      cases.map(({ field, issues }) => sortIssues(issues, field, "asc").map((issue) => issue.id))
    ).toEqual(cases.map(({ expected }) => expected));
  });

  it("reverses non-null sorts in descending order", () => {
    const issues = [
      makeIssue({ id: "c", created_at: "2025-03-01T00:00:00Z" }),
      makeIssue({ id: "a", created_at: "2025-01-01T00:00:00Z" }),
      makeIssue({ id: "b", created_at: "2025-02-01T00:00:00Z" }),
    ];

    expect(sortIssues(issues, "created_at", "desc").map((issue) => issue.id)).toEqual(["c", "b", "a"]);
  });

  it("keeps missing start_date values at the end for ascending order", () => {
    const issues = [
      makeIssue({ id: "missing-null", start_date: null }),
      makeIssue({ id: "dated", start_date: "2025-01-01T00:00:00Z" }),
      makeIssue({ id: "missing-undefined", start_date: undefined as never }),
    ];

    expect(sortIssues(issues, "start_date", "asc").map((issue) => issue.id)).toEqual([
      "dated",
      "missing-null",
      "missing-undefined",
    ]);
  });

  it("keeps missing due_date values at the end for ascending order", () => {
    const issues = [
      makeIssue({ id: "missing-null", due_date: null }),
      makeIssue({ id: "dated", due_date: "2025-01-01T00:00:00Z" }),
      makeIssue({ id: "missing-undefined", due_date: undefined as never }),
    ];

    expect(sortIssues(issues, "due_date", "asc").map((issue) => issue.id)).toEqual([
      "dated",
      "missing-null",
      "missing-undefined",
    ]);
  });

  it("moves missing start_date values to the front in descending order", () => {
    const issues = [
      makeIssue({ id: "missing", start_date: null }),
      makeIssue({ id: "early", start_date: "2025-01-01T00:00:00Z" }),
      makeIssue({ id: "late", start_date: "2025-02-01T00:00:00Z" }),
    ];

    expect(sortIssues(issues, "start_date", "desc").map((issue) => issue.id)).toEqual([
      "missing",
      "late",
      "early",
    ]);
  });

  it("falls back to rank 99 for unknown priorities", () => {
    const issues = [
      makeIssue({ id: "unknown", priority: "p0" as Issue["priority"] }),
      makeIssue({ id: "high", priority: "high" }),
      makeIssue({ id: "none", priority: "none" }),
    ];

    expect(sortIssues(issues, "priority", "asc").map((issue) => issue.id)).toEqual([
      "high",
      "none",
      "unknown",
    ]);
  });

  it("does not mutate the input array", () => {
    const issues = [
      makeIssue({ id: "second", position: 2 }),
      makeIssue({ id: "first", position: 1 }),
    ];

    sortIssues(issues, "position", "asc");

    expect(issues.map((issue) => issue.id)).toEqual(["second", "first"]);
  });
});
