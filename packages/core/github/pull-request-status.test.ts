import { describe, expect, it } from "vitest";
import {
  derivePullRequestAlerts,
  shouldShowPullRequestStats,
  type PullRequestAlertsInput,
} from "./pull-request-status";

const base: PullRequestAlertsInput = { state: "open" };

describe("derivePullRequestAlerts", () => {
  it("raises checksFailed when any observed suite failed", () => {
    expect(derivePullRequestAlerts({ ...base, checks_failed: 1 })).toEqual({
      checksFailed: true,
      conflicts: false,
    });
  });

  it("raises conflicts when mergeable_state=dirty", () => {
    expect(derivePullRequestAlerts({ ...base, mergeable_state: "dirty" })).toEqual({
      checksFailed: false,
      conflicts: true,
    });
  });

  it("raises both alerts independently — conflicts must not mask a failure", () => {
    expect(
      derivePullRequestAlerts({ ...base, mergeable_state: "dirty", checks_failed: 2 }),
    ).toEqual({ checksFailed: true, conflicts: true });
  });

  // MUL-5180 fail-only contract: green / pending / ready are unknowable from
  // completed-suite webhooks, so no-failure inputs raise nothing at all.
  it("stays silent for passed, pending, clean, unknown, and empty inputs", () => {
    for (const input of [
      base,
      { ...base, mergeable_state: "clean" },
      { ...base, mergeable_state: "unstable" },
      { ...base, mergeable_state: null },
      { ...base, checks_failed: 0 },
    ]) {
      expect(derivePullRequestAlerts(input)).toEqual({ checksFailed: false, conflicts: false });
    }
  });

  it("suppresses both alerts on terminal PRs", () => {
    for (const state of ["closed", "merged"] as const) {
      expect(
        derivePullRequestAlerts({ state, mergeable_state: "dirty", checks_failed: 99 }),
      ).toEqual({ checksFailed: false, conflicts: false });
    }
  });

  it("keeps alerts live on draft PRs — a failing draft is still actionable", () => {
    expect(derivePullRequestAlerts({ state: "draft", checks_failed: 1 })).toEqual({
      checksFailed: true,
      conflicts: false,
    });
  });
});

describe("shouldShowPullRequestStats", () => {
  it("hides when every field is 0 or missing (legacy backend)", () => {
    expect(shouldShowPullRequestStats({})).toBe(false);
    expect(shouldShowPullRequestStats({ additions: 0, deletions: 0, changed_files: 0 })).toBe(false);
  });

  it("shows when at least one number is non-zero", () => {
    expect(shouldShowPullRequestStats({ additions: 1 })).toBe(true);
    expect(shouldShowPullRequestStats({ deletions: 1 })).toBe(true);
    expect(shouldShowPullRequestStats({ changed_files: 1 })).toBe(true);
    expect(shouldShowPullRequestStats({ additions: 437, deletions: 6, changed_files: 6 })).toBe(true);
  });
});
