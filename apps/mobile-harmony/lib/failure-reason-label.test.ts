// @vitest-environment node
import { describe, expect, it } from "vitest";

import { failureReasonLabel } from "./failure-reason-label";

describe("failureReasonLabel", () => {
  it("uses actionable recovery copy for persisted runtime access denial", () => {
    const label = failureReasonLabel("runtime_access_denied");
    expect(label).toMatch(/make the runtime public/i);
    expect(label).toMatch(/rebind\/copy/i);
    expect(label).not.toBe("Failed");
  });
});
