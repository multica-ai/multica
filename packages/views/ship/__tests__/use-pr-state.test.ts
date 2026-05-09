import { describe, expect, it } from "vitest";
import type { PullRequest } from "@multica/core/types";
import {
  bucketPullRequests,
  deriveRiskHint,
  deriveShipKanbanColumn,
  isFailingOrBlocked,
} from "../hooks/use-pr-state";

// Pure logic — pull-request → Kanban column derivation. The Kanban falls
// over fast if these predicates drift from the spec, so each branch
// (drafted / in_review / ready_to_land / recently_merged / off-board)
// gets a focused assertion.

function makePR(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    repo_url: "https://github.com/acme/app",
    number: 42,
    title: "Add feature",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "deadbee",
    html_url: "https://github.com/acme/app/pull/42",
    body: null,
    ci_status: "",
    review_decision: "",
    mergeable: "MERGEABLE",
    additions: 10,
    deletions: 2,
    changed_files: 3,
    labels: [],
    pr_created_at: "2026-05-01T00:00:00Z",
    pr_updated_at: "2026-05-08T00:00:00Z",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "2026-05-09T00:00:00Z",
    ...overrides,
  };
}

const NOW = new Date("2026-05-09T12:00:00Z");

describe("deriveShipKanbanColumn", () => {
  it("open + draft → drafted", () => {
    expect(deriveShipKanbanColumn(makePR({ is_draft: true }), NOW)).toBe(
      "drafted",
    );
  });

  it("open + not draft + empty review_decision → in_review (Phase 1 graceful degrade)", () => {
    // Backend doesn't enrich review_decision yet; an empty string must
    // not trip the "Ready to Land" branch.
    expect(deriveShipKanbanColumn(makePR({ review_decision: "" }), NOW)).toBe(
      "in_review",
    );
  });

  it("open + REVIEW_REQUIRED → in_review", () => {
    expect(
      deriveShipKanbanColumn(makePR({ review_decision: "REVIEW_REQUIRED" }), NOW),
    ).toBe("in_review");
  });

  it("open + APPROVED + non-failure CI → ready_to_land", () => {
    expect(
      deriveShipKanbanColumn(
        makePR({ review_decision: "APPROVED", ci_status: "success" }),
        NOW,
      ),
    ).toBe("ready_to_land");
  });

  it("open + APPROVED but CI failing → falls back to in_review", () => {
    // We never put a failing-CI PR in Ready to Land, even if approved —
    // the failing/blocked rail also surfaces it but the column placement
    // shouldn't be celebratory.
    expect(
      deriveShipKanbanColumn(
        makePR({ review_decision: "APPROVED", ci_status: "failure" }),
        NOW,
      ),
    ).toBe("in_review");
  });

  it("merged within 7 days → recently_merged", () => {
    const recent = makePR({
      state: "merged",
      pr_merged_at: "2026-05-05T00:00:00Z",
    });
    expect(deriveShipKanbanColumn(recent, NOW)).toBe("recently_merged");
  });

  it("merged more than 7 days ago → null (off the board)", () => {
    const stale = makePR({
      state: "merged",
      pr_merged_at: "2026-04-01T00:00:00Z",
    });
    expect(deriveShipKanbanColumn(stale, NOW)).toBeNull();
  });

  it("closed without merge → null", () => {
    const closed = makePR({ state: "closed" });
    expect(deriveShipKanbanColumn(closed, NOW)).toBeNull();
  });
});

describe("isFailingOrBlocked", () => {
  it("failing CI → true (only when open)", () => {
    expect(isFailingOrBlocked(makePR({ ci_status: "failure" }))).toBe(true);
    // Failing CI on a closed PR is no longer the user's problem.
    expect(
      isFailingOrBlocked(makePR({ state: "closed", ci_status: "failure" })),
    ).toBe(false);
  });

  it("merge conflict → true", () => {
    expect(isFailingOrBlocked(makePR({ mergeable: "CONFLICTING" }))).toBe(true);
  });

  it("MERGEABLE + passing CI → false", () => {
    expect(
      isFailingOrBlocked(makePR({ mergeable: "MERGEABLE", ci_status: "success" })),
    ).toBe(false);
  });
});

describe("deriveRiskHint", () => {
  it("title contains 'migration' → 'migration'", () => {
    expect(deriveRiskHint(makePR({ title: "Add user_id migration" }))).toBe(
      "migration",
    );
  });

  it("label name 'schema' matches case-insensitively", () => {
    const pr = makePR({ labels: [{ name: "Schema", color: "ff0000" }] });
    expect(deriveRiskHint(pr)).toBe("schema");
  });

  it("no risk keywords → null", () => {
    expect(deriveRiskHint(makePR({ title: "Add a button" }))).toBeNull();
  });
});

describe("bucketPullRequests", () => {
  it("buckets open / merged / blocked PRs into separate lanes", () => {
    const draft = makePR({ id: "draft", is_draft: true });
    const inReview = makePR({ id: "review" });
    const ready = makePR({
      id: "ready",
      review_decision: "APPROVED",
      ci_status: "success",
    });
    const merged = makePR({
      id: "merged",
      state: "merged",
      pr_merged_at: "2026-05-08T00:00:00Z",
    });
    const failing = makePR({ id: "failing", ci_status: "failure" });
    const buckets = bucketPullRequests(
      [draft, inReview, ready, merged, failing],
      NOW,
    );
    expect(buckets.drafted.map((p) => p.id)).toEqual(["draft"]);
    // Failing PR also lives in `in_review` — it's still an open PR awaiting
    // attention, just also surfaced in the failing rail.
    expect(buckets.in_review.map((p) => p.id).sort()).toEqual(
      ["failing", "review"].sort(),
    );
    expect(buckets.ready_to_land.map((p) => p.id)).toEqual(["ready"]);
    expect(buckets.recently_merged.map((p) => p.id)).toEqual(["merged"]);
    expect(buckets.failing_blocked.map((p) => p.id)).toEqual(["failing"]);
  });
});
