import { describe, expect, it } from "vitest";
import type { Issue } from "@/shared/types";
import {
  deriveBacklogIssues,
  deriveTodayIssues,
  deriveUpcomingIssues,
  formatIssueSchedule,
  isIssueScheduleOverdue,
} from "./workbench-view";

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
    position: 0,
    due_date: null,
    start_date: null,
    end_date: null,
    created_at: "2026-04-11T08:00:00Z",
    updated_at: "2026-04-11T08:00:00Z",
    ...overrides,
    project_id: overrides.project_id ?? null,
  };
}

describe("workbench view helpers", () => {
  const now = new Date("2026-04-11T12:00:00Z");

  it("derives backlog issues from backlog status", () => {
    const issues = [
      makeIssue({ id: "1", status: "backlog" }),
      makeIssue({ id: "2", status: "todo" }),
    ];

    expect(deriveBacklogIssues(issues).map((issue) => issue.id)).toEqual(["1"]);
  });

  it("derives today issues from due_date, start_date, end_date, and schedule windows", () => {
    const issues = [
      makeIssue({ id: "1", due_date: "2026-04-11T09:00:00Z" }),
      makeIssue({ id: "2", start_date: "2026-04-11T08:00:00Z" }),
      makeIssue({ id: "3", end_date: "2026-04-11T09:00:00Z" }),
      makeIssue({ id: "4", start_date: "2026-04-10T00:00:00Z", end_date: "2026-04-12T00:00:00Z" }),
      makeIssue({ id: "5", due_date: "2026-04-12T09:00:00Z" }),
      makeIssue({ id: "6", due_date: "2026-04-11T09:00:00Z", status: "done" }),
    ];

    expect(deriveTodayIssues(issues, now).map((issue) => issue.id)).toEqual(["1", "2", "3", "4"]);
  });

  it("derives upcoming issues after today and excludes today overlap", () => {
    const issues = [
      makeIssue({ id: "1", due_date: "2026-04-12T09:00:00Z" }),
      makeIssue({ id: "2", start_date: "2026-04-13T00:00:00Z" }),
      makeIssue({ id: "3", start_date: "2026-04-10T00:00:00Z", end_date: "2026-04-12T00:00:00Z" }),
      makeIssue({ id: "4", due_date: "2026-04-11T09:00:00Z" }),
    ];

    expect(deriveUpcomingIssues(issues, now).map((issue) => issue.id)).toEqual(["1", "2"]);
  });

  it("formats compact issue schedule labels", () => {
    expect(
      formatIssueSchedule(
        makeIssue({ start_date: "2026-04-11T09:00:00Z", end_date: "2026-04-14T09:00:00Z" }),
      ),
    ).toBe("Apr 11 - Apr 14");

    expect(formatIssueSchedule(makeIssue({ due_date: "2026-04-11T09:00:00Z" }))).toBe("Due Apr 11");
  });

  it("marks due dates in the past as overdue", () => {
    expect(
      isIssueScheduleOverdue(makeIssue({ due_date: "2026-04-10T09:00:00Z" }), now),
    ).toBe(true);
    expect(
      isIssueScheduleOverdue(makeIssue({ due_date: "2026-04-12T09:00:00Z" }), now),
    ).toBe(false);
  });
});