import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { lifeOSFocusForIssue } from "./lifeos-focus-strip";

function issue(overrides: Partial<Issue>): Issue {
  return {
    id: "issue-1",
    workspace_id: "workspace-1",
    number: 1,
    identifier: "LIFE-1",
    title: "任务",
    description: null,
    status: "backlog",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "agent",
    creator_id: "agent-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    labels: [],
    created_at: "2026-08-30T00:00:00Z",
    updated_at: "2026-08-30T00:00:00Z",
    ...overrides,
  };
}

describe("lifeOSFocusForIssue", () => {
  it("prefers the controller's explicit attention contract", () => {
    expect(
      lifeOSFocusForIssue(
        issue({
          status: "blocked",
          metadata: { lifeos_attention_type: "decision" },
        }),
      ),
    ).toBe("decision");
  });

  it("keeps legacy cards useful before metadata backfill", () => {
    expect(
      lifeOSFocusForIssue(
        issue({ status: "in_progress", assignee_type: "agent" }),
      ),
    ).toBe("ai_working");
    expect(
      lifeOSFocusForIssue(
        issue({ status: "blocked", assignee_type: "member" }),
      ),
    ).toBe("chairman_action");
    expect(lifeOSFocusForIssue(issue({ status: "in_review" }))).toBe(
      "review",
    );
  });
});
