import { describe, expect, it } from "vitest";
import { failureReasonLabel } from "./failure-reason-label";
import { runFailureBadgeLabel } from "./run-failure-badge";

describe("runtime access revocation", () => {
  it("explains cancellation and recovery rather than reporting a runner failure", () => {
    expect(failureReasonLabel("runtime_access_revoked")).toBe(
      "Runtime access was revoked. Bind the agent to an accessible runtime before starting another run.",
    );
    expect(runFailureBadgeLabel("runtime_access_revoked")).toBe("Access revoked");
  });

  it("preserves the installed client's fallback for unknown reasons", () => {
    expect(failureReasonLabel("new_reason")).toBe("Failed");
    expect(runFailureBadgeLabel("new_reason")).toBeUndefined();
  });
});
