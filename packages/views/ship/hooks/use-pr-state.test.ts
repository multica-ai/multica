// Phase 5 — frontend risk derivation tests. The Phase 1 keyword scan
// was replaced by reading the server-classified `risk_level` and
// `risk_reasons` fields off the PR row; deriveRiskHint now returns
// either a `{ level, reasons }` shape or null (no chip).
//
// These tests pin the rendering rules:
//   - low → no chip
//   - medium with no reasons → no chip (default for unclassified PRs)
//   - medium with reasons → chip
//   - high / critical → always a chip

import { describe, it, expect } from "vitest";
import type { PullRequest } from "@multica/core/types";
import { deriveRiskHint } from "./use-pr-state";

function makePR(overrides: Partial<PullRequest>): PullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    project_id: "proj-1",
    repo_url: "https://github.com/x/y",
    number: 1,
    title: "T",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "deadbeef",
    html_url: "https://example.com/pr/1",
    body: null,
    ci_status: "",
    review_decision: "",
    mergeable: "MERGEABLE",
    additions: 0,
    deletions: 0,
    changed_files: 0,
    labels: [],
    pr_created_at: "",
    pr_updated_at: "",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "",
    ...overrides,
  };
}

describe("deriveRiskHint", () => {
  it("returns null for low risk (no chip clutter)", () => {
    const pr = makePR({ risk_level: "low", risk_reasons: ["docs only"] });
    expect(deriveRiskHint(pr)).toBeNull();
  });

  it("returns null for medium risk with no reasons", () => {
    const pr = makePR({ risk_level: "medium", risk_reasons: [] });
    expect(deriveRiskHint(pr)).toBeNull();
  });

  it("returns null when risk_level is missing entirely (older backend)", () => {
    const pr = makePR({});
    expect(deriveRiskHint(pr)).toBeNull();
  });

  it("returns a medium chip when reasons are present", () => {
    const pr = makePR({
      risk_level: "medium",
      risk_reasons: ["title mentions migration"],
    });
    const result = deriveRiskHint(pr);
    expect(result).toEqual({
      level: "medium",
      reasons: ["title mentions migration"],
    });
  });

  it("returns a high chip with reasons", () => {
    const pr = makePR({
      risk_level: "high",
      risk_reasons: ["migration file: 083_x.up.sql", "auth handler change"],
    });
    const result = deriveRiskHint(pr);
    expect(result?.level).toBe("high");
    expect(result?.reasons).toHaveLength(2);
  });

  it("returns critical for the worst tier", () => {
    const pr = makePR({
      risk_level: "critical",
      risk_reasons: ["DROP TABLE in migration"],
    });
    const result = deriveRiskHint(pr);
    expect(result?.level).toBe("critical");
  });

  it("falls back to medium-with-reasons for unknown enum values", () => {
    // Per CLAUDE.md "API Response Compatibility": an unknown server enum
    // should NOT crash — we treat unknowns as if they were medium.
    const pr = makePR({
      risk_level: "blocker", // hypothetical future value
      risk_reasons: ["unknown reason"],
    });
    const result = deriveRiskHint(pr);
    expect(result?.level).toBe("medium");
  });
});
