import { describe, it, expect } from "vitest";
import type { PullRequest } from "@multica/core/types";
import { derivePrChips } from "../hooks/use-pr-chips";

const NOW = new Date("2026-05-09T12:00:00Z");

function makePR(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    repo_url: "https://github.com/acme/app",
    number: 1234,
    title: "Refactor",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "deadbee",
    html_url: "https://github.com/acme/app/pull/1234",
    body: null,
    ci_status: "success",
    review_decision: "APPROVED",
    mergeable: "MERGEABLE",
    additions: 10,
    deletions: 1,
    changed_files: 2,
    labels: [],
    pr_created_at: "2026-05-01T00:00:00Z",
    pr_updated_at: "2026-05-09T10:00:00Z",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "2026-05-09T11:00:00Z",
    ...overrides,
  };
}

describe("derivePrChips — Phase 4 talk_to_agent chip", () => {
  it("includes talk_to_agent when originating_agent_task_id is set", () => {
    const chips = derivePrChips(
      makePR({ originating_agent_task_id: "task-7" }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "talk_to_agent")).toBe(true);
  });

  it("does NOT include talk_to_agent when originating_agent_task_id is null", () => {
    const chips = derivePrChips(makePR({ originating_agent_task_id: null }), {
      now: NOW,
    });
    expect(chips.some((c) => c.action === "talk_to_agent")).toBe(false);
  });

  it("includes talk_to_agent for merged PRs (state-agnostic)", () => {
    // The chip remains useful after merge — the user can still ask the
    // agent follow-up questions about what shipped.
    const chips = derivePrChips(
      makePR({
        state: "merged",
        pr_merged_at: "2026-05-09T11:30:00Z",
        originating_agent_task_id: "task-9",
      }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "talk_to_agent")).toBe(true);
  });
});

describe("derivePrChips — Phase 4 pull_into_issue chip", () => {
  it("appears for external_tool PRs without an issue link", () => {
    const chips = derivePrChips(
      makePR({ source: "external_tool", originating_issue_id: null }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "pull_into_issue")).toBe(true);
  });

  it("does NOT appear when the PR already has an originating issue", () => {
    const chips = derivePrChips(
      makePR({ source: "external_tool", originating_issue_id: "issue-1" }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "pull_into_issue")).toBe(false);
  });

  it("does NOT appear for multica_agent PRs", () => {
    const chips = derivePrChips(
      makePR({
        source: "multica_agent",
        originating_agent_task_id: "task-2",
      }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "pull_into_issue")).toBe(false);
  });
});
