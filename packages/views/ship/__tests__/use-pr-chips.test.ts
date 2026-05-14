import { describe, it, expect } from "vitest";
import type { PullRequest } from "@multica/core/types";
import { derivePrChips } from "../hooks/use-pr-chips";

// Anchor "now" so the 5-day stale and 24h post-merge windows are
// deterministic. This date is well after `pr_created_at` in the fixture
// so relative offsets in the per-test overrides work as written.
const NOW = new Date("2026-05-09T12:00:00Z");

function makePR(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    repo_url: "https://github.com/acme/app",
    number: 1234,
    title: "Memory KB UI",
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
    review_decision: "",
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

describe("derivePrChips priority order", () => {
  it("returns no chips for closed (non-merged) PRs", () => {
    const chips = derivePrChips(
      makePR({ state: "closed", pr_closed_at: "2026-05-09T10:00:00Z" }),
      { now: NOW },
    );
    expect(chips).toEqual([]);
  });

  it("surfaces 'Diagnose CI failure' first when CI is failing", () => {
    const chips = derivePrChips(
      makePR({ ci_status: "failure" }),
      { now: NOW },
    );
    // First chip MUST be the diagnose chip — the spec ranks CI failure
    // above every other rule.
    expect(chips[0]?.action).toBe("diagnose_ci_failure");
  });

  it("surfaces 'Rebase on main' for a conflicting PR", () => {
    const chips = derivePrChips(
      makePR({ mergeable: "CONFLICTING" }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "rebase_on_main")).toBe(true);
  });

  it("surfaces 'Merge' only when approved + green + open + non-draft + mergeable", () => {
    const chips = derivePrChips(
      makePR({
        ci_status: "success",
        review_decision: "APPROVED",
        mergeable: "MERGEABLE",
        is_draft: false,
      }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "merge")).toBe(true);
  });

  it("does NOT surface 'Merge' on a draft PR even when approved + green", () => {
    const chips = derivePrChips(
      makePR({
        ci_status: "success",
        review_decision: "APPROVED",
        mergeable: "MERGEABLE",
        is_draft: true,
      }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "merge")).toBe(false);
  });

  it("surfaces 'Summarize feedback' when review_decision is CHANGES_REQUESTED", () => {
    const chips = derivePrChips(
      makePR({ review_decision: "CHANGES_REQUESTED" }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "summarize_review_feedback")).toBe(true);
  });

  it("surfaces 'Nudge author' when an open PR hasn't been updated in 5+ days", () => {
    // Set pr_updated_at well past the 5-day window vs NOW.
    const chips = derivePrChips(
      makePR({ pr_updated_at: "2026-05-01T00:00:00Z" }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "nudge_author")).toBe(true);
  });

  it("does NOT surface 'Nudge author' on a draft PR", () => {
    const chips = derivePrChips(
      makePR({
        is_draft: true,
        pr_updated_at: "2026-05-01T00:00:00Z",
      }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "nudge_author")).toBe(false);
  });

  it("surfaces 'Run smoke tests' for a merged PR not yet on staging (24h+ ago)", () => {
    const chips = derivePrChips(
      makePR({
        state: "merged",
        head_sha: "deadbee",
        pr_merged_at: "2026-05-07T00:00:00Z", // ~2 days before NOW
      }),
      {
        now: NOW,
        stagingEnv: { id: "env-staging-1", current_sha: "olderSha" },
      },
    );
    expect(chips.some((c) => c.action === "run_smoke_tests")).toBe(true);
  });

  it("does NOT surface 'Run smoke tests' when staging already runs the head SHA", () => {
    const chips = derivePrChips(
      makePR({
        state: "merged",
        head_sha: "deadbee",
        pr_merged_at: "2026-05-07T00:00:00Z",
      }),
      {
        now: NOW,
        stagingEnv: { id: "env-staging-1", current_sha: "deadbee" },
      },
    );
    expect(chips.some((c) => c.action === "run_smoke_tests")).toBe(false);
  });

  it("does NOT surface 'Run smoke tests' for a PR merged less than 24h ago", () => {
    const chips = derivePrChips(
      makePR({
        state: "merged",
        head_sha: "deadbee",
        // 1h before NOW — well under the 24h grace window.
        pr_merged_at: "2026-05-09T11:00:00Z",
      }),
      {
        now: NOW,
        stagingEnv: { id: "env-staging-1", current_sha: "olderSha" },
      },
    );
    expect(chips.some((c) => c.action === "run_smoke_tests")).toBe(false);
  });

  it("returns chips in priority order when multiple rules match", () => {
    // CI failing (rule 1) and conflicting (rule 2). CI failure must come
    // first regardless of how many other rules match.
    const chips = derivePrChips(
      makePR({ ci_status: "failure", mergeable: "CONFLICTING" }),
      { now: NOW },
    );
    expect(chips[0]?.action).toBe("diagnose_ci_failure");
    expect(chips[1]?.action).toBe("rebase_on_main");
  });

  it("surfaces 'Review' for any open non-draft PR", () => {
    // Phase 6.5 — Review chip is state-coupled to open + non-draft.
    const chips = derivePrChips(makePR(), { now: NOW });
    expect(chips.some((c) => c.action === "submit_review")).toBe(true);
  });

  it("does NOT surface 'Review' on a draft PR", () => {
    // GitHub doesn't accept reviews on drafts; offering the chip would
    // result in a guaranteed 422.
    const chips = derivePrChips(makePR({ is_draft: true }), { now: NOW });
    expect(chips.some((c) => c.action === "submit_review")).toBe(false);
  });

  it("surfaces 'Close PR' for any open PR", () => {
    const chips = derivePrChips(makePR({ state: "open", is_draft: true }), {
      now: NOW,
    });
    expect(chips.some((c) => c.action === "close_pr")).toBe(true);
  });

  it("does NOT surface 'Close PR' for merged PRs", () => {
    const chips = derivePrChips(
      makePR({ state: "merged", pr_merged_at: "2026-05-09T10:00:00Z" }),
      { now: NOW },
    );
    expect(chips.some((c) => c.action === "close_pr")).toBe(false);
  });

  it("does NOT surface 'Review' on a closed or merged PR", () => {
    // Closed-not-merged returns no chips at all (existing rule).
    expect(
      derivePrChips(
        makePR({ state: "closed", pr_closed_at: "2026-05-09T10:00:00Z" }),
        { now: NOW },
      ).some((c) => c.action === "submit_review"),
    ).toBe(false);

    // Merged PRs get smoke-tests / talk-to-agent chips but never review.
    expect(
      derivePrChips(
        makePR({
          state: "merged",
          head_sha: "deadbee",
          pr_merged_at: "2026-05-07T00:00:00Z",
        }),
        {
          now: NOW,
          stagingEnv: { id: "env-staging-1", current_sha: "older" },
        },
      ).some((c) => c.action === "submit_review"),
    ).toBe(false);
  });

  it("marks the Review chip as custom (opens a dialog, not a mutation)", () => {
    const chips = derivePrChips(makePR(), { now: NOW });
    const review = chips.find((c) => c.action === "submit_review");
    expect(review?.custom).toBe(true);
  });

  it("attaches an environment_id body builder to the smoke-test chip", () => {
    const chips = derivePrChips(
      makePR({
        state: "merged",
        head_sha: "deadbee",
        pr_merged_at: "2026-05-07T00:00:00Z",
      }),
      {
        now: NOW,
        stagingEnv: { id: "env-staging-1", current_sha: "older" },
      },
    );
    const smoke = chips.find((c) => c.action === "run_smoke_tests");
    expect(smoke).toBeDefined();
    expect(smoke?.bodyBuilder?.(makePR())).toEqual({
      environment_id: "env-staging-1",
    });
  });
});
