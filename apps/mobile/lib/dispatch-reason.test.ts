// @vitest-environment node
import { describe, expect, it } from "vitest";
import { dispatchReasonCode, sendFailureMessage } from "./dispatch-reason";

// Shaped like `apps/mobile/data/api.ts:ApiError` — a thrown Error carrying the
// parsed response body. Built inline rather than imported because `@/data/api`
// pulls react-native into a node-environment suite; the only field under test
// is `body`, which is what the helper reads.
const apiError = (body: unknown) => Object.assign(new Error("request failed"), { body });

describe("dispatchReasonCode", () => {
  it("reads reason_code off a structured rejection body", () => {
    const err = apiError({
      error: "invocation not allowed",
      reason_code: "invocation_not_allowed",
    });
    expect(dispatchReasonCode(err)).toBe("invocation_not_allowed");
  });

  it("returns undefined for an unstructured failure", () => {
    expect(dispatchReasonCode(new Error("network down"))).toBeUndefined();
    expect(dispatchReasonCode(apiError(undefined))).toBeUndefined();
    expect(dispatchReasonCode(apiError("plain text body"))).toBeUndefined();
    expect(dispatchReasonCode(null)).toBeUndefined();
  });

  it("ignores a non-string or empty reason_code", () => {
    expect(dispatchReasonCode(apiError({ reason_code: 7 }))).toBeUndefined();
    expect(dispatchReasonCode(apiError({ reason_code: "" }))).toBeUndefined();
  });
});

describe("sendFailureMessage", () => {
  // The point of the helper: a revoked permission must not read as a transient
  // failure the user should retry (MUL-6380).
  it("names revoked permission instead of suggesting a retry", () => {
    const message = sendFailureMessage(
      apiError({ reason_code: "invocation_not_allowed" }),
    );
    expect(message).toMatch(/no longer have permission/i);
    expect(message).not.toMatch(/try again/i);
  });

  it("names the runtime for agent_runtime_required", () => {
    expect(
      sendFailureMessage(apiError({ reason_code: "agent_runtime_required" })),
    ).toMatch(/runtime/i);
  });

  it("falls back to a retryable message for anything else", () => {
    expect(sendFailureMessage(new Error("timeout"))).toMatch(/try again/i);
  });
});
