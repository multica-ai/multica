import { describe, expect, it } from "vitest";
import {
  derivePullRequestStatusKind,
  derivePullRequestProgressSegments,
  shouldShowPullRequestStats,
  type PullRequestStatusInput,
} from "./pull-request-status";

const base: PullRequestStatusInput = { state: "open" };

describe("derivePullRequestStatusKind", () => {
  it("closed beats every other signal", () => {
    expect(
      derivePullRequestStatusKind({
        state: "closed",
        mergeable_state: "dirty",
        checks_failed: 99,
        checks_pending: 99,
        checks_passed: 99,
      }),
    ).toBe("closed");
  });

  it("merged beats every other signal except closed", () => {
    expect(
      derivePullRequestStatusKind({
        state: "merged",
        mergeable_state: "dirty",
        checks_failed: 5,
      }),
    ).toBe("merged");
  });

  it("dirty conflicts wins over check signals", () => {
    expect(
      derivePullRequestStatusKind({
        ...base,
        mergeable_state: "dirty",
        checks_passed: 3,
      }),
    ).toBe("conflicts");
  });

  it("any failed check beats pending and passed", () => {
    expect(
      derivePullRequestStatusKind({
        ...base,
        checks_failed: 1,
        checks_pending: 3,
        checks_passed: 5,
      }),
    ).toBe("checks_failed");
  });

  it("pending beats passed when no failure", () => {
    expect(
      derivePullRequestStatusKind({
        ...base,
        checks_pending: 1,
        checks_passed: 5,
      }),
    ).toBe("checks_pending");
  });

  it("all-passed is checks_passed regardless of mergeable=clean", () => {
    expect(
      derivePullRequestStatusKind({
        ...base,
        mergeable_state: "clean",
        checks_passed: 5,
      }),
    ).toBe("checks_passed");
  });

  it("clean + no suites is ready-to-merge", () => {
    expect(
      derivePullRequestStatusKind({ ...base, mergeable_state: "clean" }),
    ).toBe("ready");
  });

  it("opaque mergeable values render as unknown", () => {
    for (const m of ["blocked", "behind", "unstable", "has_hooks", "unknown", null, undefined]) {
      expect(derivePullRequestStatusKind({ ...base, mergeable_state: m })).toBe("unknown");
    }
  });
});

describe("derivePullRequestProgressSegments", () => {
  it("returns null for terminal PRs (merged / closed)", () => {
    expect(derivePullRequestProgressSegments({ state: "merged", checks_passed: 5 })).toBeNull();
    expect(derivePullRequestProgressSegments({ state: "closed", checks_failed: 3 })).toBeNull();
  });

  it("returns null when no suite has been observed", () => {
    expect(derivePullRequestProgressSegments({ ...base })).toBeNull();
    expect(
      derivePullRequestProgressSegments({ ...base, checks_failed: 0, checks_pending: 0, checks_passed: 0 }),
    ).toBeNull();
  });

  it("orders segments failed → pending → passed (failure leftmost)", () => {
    const segs = derivePullRequestProgressSegments({
      ...base,
      checks_failed: 1,
      checks_pending: 2,
      checks_passed: 3,
    });
    expect(segs).not.toBeNull();
    expect(segs!.map((s) => s.kind)).toEqual(["failed", "pending", "passed"]);
  });

  it("emits a zero-width segment-free output (no entry with ratio 0)", () => {
    const segs = derivePullRequestProgressSegments({
      ...base,
      checks_failed: 0,
      checks_pending: 0,
      checks_passed: 4,
    });
    expect(segs).toEqual([{ kind: "passed", ratio: 1 }]);
  });

  it("ratios sum to ~1 across segments", () => {
    const segs = derivePullRequestProgressSegments({
      ...base,
      checks_failed: 1,
      checks_pending: 1,
      checks_passed: 2,
    })!;
    const total = segs.reduce((acc, s) => acc + s.ratio, 0);
    expect(total).toBeCloseTo(1, 6);
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
