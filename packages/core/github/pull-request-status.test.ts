import { describe, expect, it } from "vitest";
import {
  deriveChecksStatus,
  deriveMergeStatus,
  shouldShowPullRequestStats,
} from "./pull-request-status";

describe("deriveChecksStatus", () => {
  it("maps a `failure` rollup to failed and carries counts + names", () => {
    expect(
      deriveChecksStatus({
        checks_rollup: "failure",
        checks_total: 7,
        checks_failed: 2,
        failed_check_names: ["backend", "e2e"],
      }),
    ).toEqual({ kind: "failed", failed: 2, total: 7, names: ["backend", "e2e"] });
  });

  it("maps an `error` rollup to failed", () => {
    expect(deriveChecksStatus({ checks_rollup: "error", checks_total: 3 }).kind).toBe("failed");
  });

  it("treats any failed count as failed even when the rollup is absent", () => {
    // Failure is trusted from the count so a known failure surfaces even if the
    // rollup verdict lags.
    expect(deriveChecksStatus({ checks_failed: 1 }).kind).toBe("failed");
  });

  it("failure beats pending and passed", () => {
    expect(
      deriveChecksStatus({
        checks_rollup: "failure",
        checks_failed: 1,
        checks_running: 3,
        checks_passed: 5,
      }).kind,
    ).toBe("failed");
  });

  it("maps `pending` / `expected` rollups to pending with running count", () => {
    expect(
      deriveChecksStatus({
        checks_rollup: "pending",
        checks_total: 7,
        checks_passed: 5,
        checks_running: 2,
      }),
    ).toEqual({ kind: "pending", passed: 5, total: 7, running: 2 });
    expect(deriveChecksStatus({ checks_rollup: "expected" }).kind).toBe("pending");
  });

  it("maps a `success` rollup to passed", () => {
    expect(deriveChecksStatus({ checks_rollup: "success", checks_total: 7 })).toEqual({
      kind: "passed",
      total: 7,
    });
  });

  it("renders `none` only for a current snapshot whose rollup is absent", () => {
    expect(deriveChecksStatus({ snapshot_available: true }).kind).toBe("none");
    expect(
      deriveChecksStatus({
        snapshot_available: true,
        checks_rollup: null,
        checks_passed: 5,
        checks_total: 5,
      }).kind,
    ).toBe("none");
  });

  it("hides CI when the API snapshot is disabled or has not landed", () => {
    expect(deriveChecksStatus({}).kind).toBe("unavailable");
    expect(
      deriveChecksStatus({
        snapshot_available: false,
        checks_rollup: "success",
        checks_conclusion: "passed",
        checks_total: 5,
      }).kind,
    ).toBe("unavailable");
  });

  it("preserves legacy provider passed, pending, and failed conclusions", () => {
    expect(
      deriveChecksStatus({ checks_conclusion: "passed", checks_total: 3 }),
    ).toEqual({ kind: "passed", total: 3 });
    expect(
      deriveChecksStatus({
        checks_conclusion: "pending",
        checks_total: 3,
        checks_passed: 2,
        checks_pending: 1,
      }),
    ).toEqual({ kind: "pending", passed: 2, total: 3, running: 1 });
    expect(
      deriveChecksStatus({
        checks_conclusion: "failed",
        checks_total: 3,
        checks_failed: 1,
      }).kind,
    ).toBe("failed");
  });
});

describe("deriveMergeStatus", () => {
  it("maps `conflicting` to conflicting", () => {
    expect(deriveMergeStatus({ mergeable: "conflicting" }).kind).toBe("conflicting");
  });

  it("folds a `dirty` merge state into conflicting", () => {
    expect(deriveMergeStatus({ merge_state_status: "dirty" }).kind).toBe("conflicting");
  });

  it("asserts ready ONLY from a `clean` merge state", () => {
    expect(deriveMergeStatus({ merge_state_status: "clean" }).kind).toBe("ready");
  });

  it("never infers ready from `mergeable === mergeable` alone", () => {
    // "No conflict" is not "ready" — required checks / branch protection live in
    // merge_state_status, so mergeable without a clean state renders nothing.
    expect(deriveMergeStatus({ mergeable: "mergeable" }).kind).toBe("none");
    expect(deriveMergeStatus({ mergeable: "mergeable", merge_state_status: null }).kind).toBe("none");
  });

  it("surfaces blocked / behind / unstable / has_hooks faithfully", () => {
    expect(deriveMergeStatus({ merge_state_status: "blocked" }).kind).toBe("blocked");
    expect(deriveMergeStatus({ merge_state_status: "behind" }).kind).toBe("behind");
    expect(deriveMergeStatus({ merge_state_status: "unstable" }).kind).toBe("unstable");
    expect(deriveMergeStatus({ merge_state_status: "has_hooks" }).kind).toBe("has_hooks");
  });

  it("renders nothing when GitHub has not decided", () => {
    // unknown / null shows neither conflict nor ready.
    expect(deriveMergeStatus({}).kind).toBe("none");
    expect(deriveMergeStatus({ mergeable: "unknown" }).kind).toBe("none");
    expect(deriveMergeStatus({ mergeable: null, merge_state_status: "unknown" }).kind).toBe("none");
    expect(deriveMergeStatus({ merge_state_status: "draft" }).kind).toBe("none");
  });

  it("renders nothing when the API snapshot feature is unavailable", () => {
    expect(
      deriveMergeStatus({
        snapshot_available: false,
        mergeable: "conflicting",
        merge_state_status: "dirty",
      }).kind,
    ).toBe("none");
  });

  it("conflict wins over an otherwise decisive merge state", () => {
    expect(
      deriveMergeStatus({ mergeable: "conflicting", merge_state_status: "blocked" }).kind,
    ).toBe("conflicting");
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
