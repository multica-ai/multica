// @vitest-environment node
import { describe, expect, it } from "vitest";

import { cancelReasonLabel, failureReasonLabel } from "./task-failure";

// cancelReasonLabel decides which cancelled rows explain themselves. The rule
// it must hold: a SERVER-cancelled row (persisted reason) reads like a failed
// row, a user's own cancel stays a plain "Cancelled" — labelling every cancel
// would bury the rows that actually need the user to act.
describe("cancelReasonLabel", () => {
  it("returns null for a user-initiated cancel", () => {
    expect(cancelReasonLabel({ status: "cancelled", error: null, failure_reason: null })).toBeNull();
  });

  it("only ever labels cancelled rows — failed rows have their own path", () => {
    expect(
      cancelReasonLabel({ status: "failed", error: "boom", failure_reason: "timeout" }),
    ).toBeNull();
  });

  it("labels the worktree claim gate's cancellation", () => {
    expect(
      cancelReasonLabel({
        status: "cancelled",
        error: "worktree mode needs daemon version 0.4.24 or newer",
        failure_reason: "local_directory_error",
      }),
    ).toBe("Local directory error");
  });

  it("falls back to a generic system label when only error text was persisted", () => {
    expect(
      cancelReasonLabel({
        status: "cancelled",
        error: "work preserved in the worktree at /env/worktree",
        failure_reason: null,
      }),
    ).toBe("Cancelled by the system");
  });
});

describe("failureReasonLabel", () => {
  it("covers the local_directory bucket the worktree paths write", () => {
    expect(failureReasonLabel("local_directory_error")).toBe("Local directory error");
  });

  it("still falls back to the raw wire value for unknown reasons", () => {
    expect(failureReasonLabel("brand_new_reason")).toBe("brand_new_reason");
  });
});
