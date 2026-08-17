import { describe, expect, it } from "vitest";
import { resolveFailureReasonKey } from "./failure-reason";

const COPY = {
  agent_error: "family",
  "agent_error.provider_network": "refined",
  timeout: "platform",
} as const;

describe("resolveFailureReasonKey", () => {
  it("prefers an exact match over the coarse family", () => {
    expect(resolveFailureReasonKey("agent_error.provider_network", COPY)).toBe(
      "agent_error.provider_network",
    );
  });

  it("degrades a refined reason to its family when there is no exact copy", () => {
    // The MUL-5370 regression: before this, agent_error.unknown missed the
    // lookup entirely and the UI rendered a generic fallback instead of the
    // agent_error line the backend's classification had earned.
    expect(resolveFailureReasonKey("agent_error.unknown", COPY)).toBe(
      "agent_error",
    );
  });

  it("degrades a reason the client has never seen to its family", () => {
    expect(
      resolveFailureReasonKey("agent_error.some_future_bucket", COPY),
    ).toBe("agent_error");
  });

  it("matches a platform-side reason exactly", () => {
    expect(resolveFailureReasonKey("timeout", COPY)).toBe("timeout");
  });

  it("returns undefined when neither the reason nor its family is known", () => {
    expect(resolveFailureReasonKey("skill_bundle_unavailable", COPY)).toBeUndefined();
    expect(resolveFailureReasonKey("brand_new.thing", COPY)).toBeUndefined();
  });

  it("returns undefined for empty and nullish input", () => {
    expect(resolveFailureReasonKey("", COPY)).toBeUndefined();
    expect(resolveFailureReasonKey(null, COPY)).toBeUndefined();
    expect(resolveFailureReasonKey(undefined, COPY)).toBeUndefined();
  });

  it("ignores inherited Object properties so a reason named like one cannot spoof a key", () => {
    expect(resolveFailureReasonKey("constructor", COPY)).toBeUndefined();
    expect(resolveFailureReasonKey("toString", COPY)).toBeUndefined();
  });

  it("does not treat a leading dot as a family separator", () => {
    expect(resolveFailureReasonKey(".agent_error", COPY)).toBeUndefined();
  });
});
