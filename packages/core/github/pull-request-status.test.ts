import { describe, expect, it } from "vitest";
import {
  deriveChecksStatus,
  deriveMergeStatus,
  derivePullRequestVerdict,
  formatPullRequestDiffCount,
  shouldShowPullRequestStats,
  stripIssueKeyFromTitle,
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

describe("derivePullRequestVerdict", () => {
  const open = { state: "open" as const, snapshot_available: true };

  it("ends at merged / closed whatever the snapshot says", () => {
    const failing = { checks_rollup: "failure" as const, checks_failed: 2, mergeable: "conflicting" as const };
    expect(derivePullRequestVerdict({ ...failing, state: "merged" })).toEqual({ kind: "merged" });
    expect(derivePullRequestVerdict({ ...failing, state: "closed" })).toEqual({ kind: "closed" });
  });

  it("leads with a failure and keeps the names and a conflict alongside it", () => {
    expect(
      derivePullRequestVerdict({
        ...open,
        checks_rollup: "failure",
        checks_total: 19,
        checks_failed: 2,
        failed_check_names: ["frontend", "backend"],
        mergeable: "conflicting",
      }),
    ).toEqual({ kind: "failed", failed: 2, total: 19, names: ["frontend", "backend"], conflicting: true });
  });

  it("does not let `unstable` add anything to a failure", () => {
    expect(
      derivePullRequestVerdict({ ...open, checks_rollup: "failure", checks_failed: 1, merge_state_status: "unstable" }),
    ).toMatchObject({ kind: "failed", conflicting: false });
  });

  it("puts a conflict ahead of running checks", () => {
    expect(
      derivePullRequestVerdict({ ...open, checks_rollup: "pending", checks_running: 3, mergeable: "conflicting" }).kind,
    ).toBe("conflicting");
  });

  it("reports running checks with their progress", () => {
    expect(
      derivePullRequestVerdict({ ...open, checks_rollup: "pending", checks_total: 19, checks_passed: 11, checks_running: 8 }),
    ).toEqual({ kind: "running", passed: 11, total: 19, running: 8 });
  });

  it.each([
    ["behind", "behind"],
    ["blocked", "blocked"],
    ["clean", "ready"],
    ["has_hooks", "ready"],
  ] as const)("maps passing checks with merge state %s to %s", (merge_state_status, kind) => {
    expect(
      derivePullRequestVerdict({ ...open, checks_rollup: "success", checks_total: 3, merge_state_status }).kind,
    ).toBe(kind);
  });

  it("never calls a draft ready, but still surfaces its problems", () => {
    const draft = { ...open, state: "draft" as const };
    expect(derivePullRequestVerdict({ ...draft, checks_rollup: "success", merge_state_status: "clean" }).kind).toBe("draft");
    expect(derivePullRequestVerdict({ ...draft, checks_rollup: "failure", checks_failed: 1 }).kind).toBe("failed");
  });

  it("falls back to the checks verdict when GitHub has not decided mergeability", () => {
    expect(derivePullRequestVerdict({ ...open, checks_rollup: "success", checks_total: 4 })).toEqual({ kind: "passed", total: 4 });
    expect(derivePullRequestVerdict({ ...open, checks_rollup: "success", merge_state_status: "unstable" }).kind).toBe("passed");
    expect(derivePullRequestVerdict({ ...open, checks_rollup: null }).kind).toBe("no_checks");
  });

  it("says nothing when there is no current snapshot", () => {
    expect(
      derivePullRequestVerdict({ state: "open", snapshot_available: false, checks_rollup: "failure", mergeable: "conflicting" }),
    ).toEqual({ kind: "unknown" });
    expect(derivePullRequestVerdict({ state: "open" })).toEqual({ kind: "unknown" });
  });
});

describe("stripIssueKeyFromTitle", () => {
  it.each([
    ["MUL-7732: regroup settings", "regroup settings"],
    ["MUL-7732 regroup settings", "regroup settings"],
    ["mul-7732: regroup settings", "regroup settings"],
    ["fix(chat): keep tool rows (MUL-7732)", "fix(chat): keep tool rows"],
  ])("strips the issue's own key from %j", (title, expected) => {
    expect(stripIssueKeyFromTitle(title, "MUL-7732")).toBe(expected);
  });

  it.each([
    ["MUL-77321: another issue"],
    ["MUL-7700: another issue"],
    ["regroup MUL-7732 settings"],
    ["MUL-7732"],
  ])("leaves %j as written", (title) => {
    expect(stripIssueKeyFromTitle(title, "MUL-7732")).toBe(title);
  });

  it("leaves the title alone without an identifier", () => {
    expect(stripIssueKeyFromTitle("MUL-1: x", "")).toBe("MUL-1: x");
  });
});

describe("formatPullRequestDiffCount", () => {
  it.each([
    [0, "0"],
    [999, "999"],
    [1000, "1k"],
    [6743, "6.7k"],
    [4557, "4.6k"],
    [99_960, "100k"],
    [123_456, "123k"],
  ])("formats %d as %s", (count, expected) => {
    expect(formatPullRequestDiffCount(count)).toBe(expected);
  });
});
