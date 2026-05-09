import { describe, expect, it } from "vitest";
import type { DeployEnvironment, PullRequest } from "@multica/core/types";
import {
  EMPTY_DEPLOY_SNAPSHOT,
  bucketPullRequests,
  deriveRiskHint,
  deriveShipKanbanColumn,
  isFailingOrBlocked,
  type ShipDeploySnapshot,
} from "../hooks/use-pr-state";

// Pure logic — pull-request → Kanban column derivation. The 8-column
// Phase-2 board falls over fast if these predicates drift from the spec,
// so each branch (drafted / in_review / ready_to_land / merged_pre_staging
// / in_staging / promoting / in_production / done / off-board) gets a
// focused assertion.

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

function makeEnv(overrides: Partial<DeployEnvironment> = {}): DeployEnvironment {
  return {
    id: "env-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    kind: "staging",
    name: "Staging",
    target_branch: "main",
    target_url: null,
    current_sha: null,
    current_deployed_at: null,
    auto_promote: false,
    created_at: "2026-05-01T00:00:00Z",
    updated_at: "2026-05-01T00:00:00Z",
    ...overrides,
  };
}

const NOW = new Date("2026-05-09T12:00:00Z");

describe("deriveShipKanbanColumn — open PRs", () => {
  it("open + draft → drafted", () => {
    expect(
      deriveShipKanbanColumn(makePR({ is_draft: true }), EMPTY_DEPLOY_SNAPSHOT, NOW),
    ).toBe("drafted");
  });

  it("open + not draft + empty review_decision → in_review (graceful degrade)", () => {
    // Backend may not have populated review_decision yet on a fresh
    // workspace; an empty string must not trip the "Ready to Land" branch.
    expect(
      deriveShipKanbanColumn(makePR({ review_decision: "" }), EMPTY_DEPLOY_SNAPSHOT, NOW),
    ).toBe("in_review");
  });

  it("open + REVIEW_REQUIRED → in_review", () => {
    expect(
      deriveShipKanbanColumn(
        makePR({ review_decision: "REVIEW_REQUIRED" }),
        EMPTY_DEPLOY_SNAPSHOT,
        NOW,
      ),
    ).toBe("in_review");
  });

  it("open + CHANGES_REQUESTED → in_review", () => {
    expect(
      deriveShipKanbanColumn(
        makePR({ review_decision: "CHANGES_REQUESTED" }),
        EMPTY_DEPLOY_SNAPSHOT,
        NOW,
      ),
    ).toBe("in_review");
  });

  it("open + APPROVED + non-failure CI → ready_to_land", () => {
    expect(
      deriveShipKanbanColumn(
        makePR({ review_decision: "APPROVED", ci_status: "success" }),
        EMPTY_DEPLOY_SNAPSHOT,
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
        EMPTY_DEPLOY_SNAPSHOT,
        NOW,
      ),
    ).toBe("in_review");
  });

  it("closed without merge → null", () => {
    expect(
      deriveShipKanbanColumn(makePR({ state: "closed" }), EMPTY_DEPLOY_SNAPSHOT, NOW),
    ).toBeNull();
  });
});

describe("deriveShipKanbanColumn — merged PRs through deploy lanes", () => {
  it("merged within 7d, no envs → merged_pre_staging", () => {
    const recent = makePR({
      state: "merged",
      pr_merged_at: "2026-05-05T00:00:00Z",
      head_sha: "abc1234",
    });
    expect(deriveShipKanbanColumn(recent, EMPTY_DEPLOY_SNAPSHOT, NOW)).toBe(
      "merged_pre_staging",
    );
  });

  it("merged > 7d ago, no envs → done (Phase 2 simplification)", () => {
    const stale = makePR({
      state: "merged",
      pr_merged_at: "2026-04-01T00:00:00Z",
    });
    expect(deriveShipKanbanColumn(stale, EMPTY_DEPLOY_SNAPSHOT, NOW)).toBe("done");
  });

  it("merged + head SHA matches staging.current_sha → in_staging", () => {
    const pr = makePR({
      state: "merged",
      pr_merged_at: "2026-05-08T00:00:00Z",
      head_sha: "abc1234",
    });
    const snapshot: ShipDeploySnapshot = {
      staging: makeEnv({ kind: "staging", current_sha: "abc1234" }),
      production: null,
      productionInFlightShas: new Set(),
    };
    expect(deriveShipKanbanColumn(pr, snapshot, NOW)).toBe("in_staging");
  });

  it("merged + head SHA matches in-flight production deploy → promoting", () => {
    const pr = makePR({
      state: "merged",
      pr_merged_at: "2026-05-08T00:00:00Z",
      head_sha: "abc1234",
    });
    const snapshot: ShipDeploySnapshot = {
      staging: makeEnv({ kind: "staging", current_sha: "abc1234" }),
      production: makeEnv({ id: "env-2", kind: "production", current_sha: "old-sha" }),
      productionInFlightShas: new Set(["abc1234"]),
    };
    // "Promoting" beats "in_staging" — most-advanced state wins.
    expect(deriveShipKanbanColumn(pr, snapshot, NOW)).toBe("promoting");
  });

  it("merged + head SHA matches prod.current_sha + recent prod deploy → in_production", () => {
    const pr = makePR({
      state: "merged",
      pr_merged_at: "2026-05-08T00:00:00Z",
      head_sha: "abc1234",
    });
    const snapshot: ShipDeploySnapshot = {
      staging: null,
      production: makeEnv({
        id: "env-2",
        kind: "production",
        current_sha: "abc1234",
        // Deployed 1 hour ago — well within 24h.
        current_deployed_at: "2026-05-09T11:00:00Z",
      }),
      productionInFlightShas: new Set(),
    };
    expect(deriveShipKanbanColumn(pr, snapshot, NOW)).toBe("in_production");
  });

  it("merged + on prod but deploy older than 24h → done", () => {
    const pr = makePR({
      state: "merged",
      pr_merged_at: "2026-05-07T00:00:00Z",
      head_sha: "abc1234",
    });
    const snapshot: ShipDeploySnapshot = {
      staging: null,
      production: makeEnv({
        id: "env-2",
        kind: "production",
        current_sha: "abc1234",
        // Deployed 36h ago — past the 24h "in_production" window.
        current_deployed_at: "2026-05-08T00:00:00Z",
      }),
      productionInFlightShas: new Set(),
    };
    expect(deriveShipKanbanColumn(pr, snapshot, NOW)).toBe("done");
  });

  it("merged with missing pr_merged_at → defensive merged_pre_staging", () => {
    const pr = makePR({ state: "merged", pr_merged_at: null });
    expect(deriveShipKanbanColumn(pr, EMPTY_DEPLOY_SNAPSHOT, NOW)).toBe(
      "merged_pre_staging",
    );
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

describe("bucketPullRequests — 8-column", () => {
  it("buckets open / merged / blocked PRs across the 8 columns", () => {
    const draft = makePR({ id: "draft", is_draft: true });
    const inReview = makePR({ id: "review" });
    const ready = makePR({
      id: "ready",
      review_decision: "APPROVED",
      ci_status: "success",
    });
    const mergedRecent = makePR({
      id: "merged",
      state: "merged",
      pr_merged_at: "2026-05-08T00:00:00Z",
      head_sha: "merged-sha",
    });
    const inStaging = makePR({
      id: "staging",
      state: "merged",
      pr_merged_at: "2026-05-08T00:00:00Z",
      head_sha: "staging-sha",
    });
    const promoting = makePR({
      id: "promoting",
      state: "merged",
      pr_merged_at: "2026-05-08T00:00:00Z",
      head_sha: "promoting-sha",
    });
    const inProd = makePR({
      id: "prod",
      state: "merged",
      pr_merged_at: "2026-05-08T00:00:00Z",
      head_sha: "prod-sha",
    });
    const oldMerged = makePR({
      id: "old",
      state: "merged",
      pr_merged_at: "2026-04-01T00:00:00Z",
      head_sha: "old-sha",
    });
    const failing = makePR({ id: "failing", ci_status: "failure" });

    const snapshot: ShipDeploySnapshot = {
      staging: makeEnv({ kind: "staging", current_sha: "staging-sha" }),
      production: makeEnv({
        id: "env-2",
        kind: "production",
        current_sha: "prod-sha",
        current_deployed_at: "2026-05-09T11:00:00Z",
      }),
      productionInFlightShas: new Set(["promoting-sha"]),
    };

    const buckets = bucketPullRequests(
      [draft, inReview, ready, mergedRecent, inStaging, promoting, inProd, oldMerged, failing],
      snapshot,
      NOW,
    );

    expect(buckets.drafted.map((p) => p.id)).toEqual(["draft"]);
    // Failing PR also lives in `in_review` — it's still an open PR awaiting
    // attention, just also surfaced in the failing rail.
    expect(buckets.in_review.map((p) => p.id).sort()).toEqual(
      ["failing", "review"].sort(),
    );
    expect(buckets.ready_to_land.map((p) => p.id)).toEqual(["ready"]);
    expect(buckets.merged_pre_staging.map((p) => p.id)).toEqual(["merged"]);
    expect(buckets.in_staging.map((p) => p.id)).toEqual(["staging"]);
    expect(buckets.promoting.map((p) => p.id)).toEqual(["promoting"]);
    expect(buckets.in_production.map((p) => p.id)).toEqual(["prod"]);
    expect(buckets.done.map((p) => p.id)).toEqual(["old"]);
    expect(buckets.failing_blocked.map((p) => p.id)).toEqual(["failing"]);
  });

  it("captures CI-failed and merge-conflict PRs in the failing rail", () => {
    const ciFail = makePR({ id: "ci", ci_status: "failure" });
    const conflict = makePR({ id: "conflict", mergeable: "CONFLICTING" });
    const buckets = bucketPullRequests(
      [ciFail, conflict],
      EMPTY_DEPLOY_SNAPSHOT,
      NOW,
    );
    expect(buckets.failing_blocked.map((p) => p.id).sort()).toEqual(
      ["ci", "conflict"].sort(),
    );
  });
});
