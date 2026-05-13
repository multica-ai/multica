// JEH-1152 — coverage for the structured-403 toast helper. Pure-logic test:
// we don't render anything, just assert that the helper inspects the
// ApiError body shape correctly and feeds the right values into sonner.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { ApiError } from "@multica/core/api";

const toastError = vi.hoisted(() => vi.fn());
vi.mock("sonner", () => ({
  toast: {
    error: toastError,
  },
}));

import { showRuntimeApprovalToastIfApplicable } from "./runtime-approval-toast";

const deps = {
  issuePath: (id: string) => `/test/issues/${id}`,
  push: vi.fn(),
};

beforeEach(() => {
  toastError.mockReset();
  deps.push.mockReset();
});

describe("showRuntimeApprovalToastIfApplicable", () => {
  it("returns false for non-ApiError values", () => {
    expect(showRuntimeApprovalToastIfApplicable(new Error("plain"), deps)).toBe(false);
    expect(showRuntimeApprovalToastIfApplicable("not an error", deps)).toBe(false);
    expect(showRuntimeApprovalToastIfApplicable(null, deps)).toBe(false);
    expect(toastError).not.toHaveBeenCalled();
  });

  it("returns false for non-403 ApiError", () => {
    const err = new ApiError("nope", 500, "Internal Server Error", {
      error: "nope",
      code: "runtime_not_approved_by_group",
    });
    expect(showRuntimeApprovalToastIfApplicable(err, deps)).toBe(false);
    expect(toastError).not.toHaveBeenCalled();
  });

  it("returns false for 403 with a different code", () => {
    const err = new ApiError("nope", 403, "Forbidden", {
      error: "nope",
      code: "something_else",
    });
    expect(showRuntimeApprovalToastIfApplicable(err, deps)).toBe(false);
  });

  it("renders rich toast with action when body has approval_issue", () => {
    const err = new ApiError("This runtime must be approved", 403, "Forbidden", {
      error: "This runtime must be approved",
      code: "runtime_not_approved_by_group",
      approval_issue: {
        id: "issue-uuid",
        identifier: "HAN-42",
        title: "Approve runtime: foo",
      },
    });
    expect(showRuntimeApprovalToastIfApplicable(err, deps)).toBe(true);
    expect(toastError).toHaveBeenCalledTimes(1);
    const call = toastError.mock.calls[0];
    if (!call) throw new Error("toast.error was not called");
    const [message, opts] = call;
    expect(message).toBe("This runtime must be approved");
    expect(opts.description).toContain("HAN-42");
    expect(opts.description).toContain("Approve runtime: foo");

    // Action click navigates to the issue.
    opts.action.onClick();
    expect(deps.push).toHaveBeenCalledWith("/test/issues/issue-uuid");
  });

  it("falls back to plain toast when approval_issue is missing or malformed", () => {
    const err = new ApiError("This runtime must be approved", 403, "Forbidden", {
      error: "This runtime must be approved",
      code: "runtime_not_approved_by_group",
    });
    expect(showRuntimeApprovalToastIfApplicable(err, deps)).toBe(true);
    expect(toastError).toHaveBeenCalledWith("This runtime must be approved");

    // Malformed approval_issue (missing fields) treated the same.
    toastError.mockReset();
    const err2 = new ApiError("msg", 403, "Forbidden", {
      error: "msg",
      code: "runtime_not_approved_by_group",
      approval_issue: { id: "x" }, // missing identifier + title
    });
    expect(showRuntimeApprovalToastIfApplicable(err2, deps)).toBe(true);
    expect(toastError).toHaveBeenCalledWith("msg");
  });
});
